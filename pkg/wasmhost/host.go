package wasmhost

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/stealthrocket/wasi-go/imports"
	"github.com/stealthrocket/wasi-go/imports/wasi_http"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"
)

// WasmHost manages a single wazero runtime and a cache of compiled modules.
type WasmHost struct {
	runtime        wazero.Runtime
	cache          map[string]wazero.CompiledModule
	wasiHTTPLoaded bool
	wasiSystem     interface{} // Store the WASI system instance
	mu             sync.RWMutex
}

// NewWasmHost creates a new host for running WASM modules.
func NewWasmHost(ctx context.Context) *WasmHost {
	runtime := wazero.NewRuntime(ctx)
	return &WasmHost{
		runtime:        runtime,
		cache:          make(map[string]wazero.CompiledModule),
		wasiHTTPLoaded: false,
		wasiSystem:     nil,
	}
}

// NewLoggingWasmHost creates a new host for running WASM modules with HTTP request logging.
func NewLoggingWasmHost(ctx context.Context) *WasmHost {
	runtime := wazero.NewRuntime(ctx)
	return &WasmHost{
		runtime:        runtime,
		cache:          make(map[string]wazero.CompiledModule),
		wasiHTTPLoaded: false,
		wasiSystem:     nil,
	}
}

// RunModuleWithLogging runs a module with comprehensive logging of all I/O operations
func (h *WasmHost) RunModuleWithLogging(ctx context.Context, name string, envVars ...string) (io.Reader, io.Writer, error) {
	log.Printf("[WASM HOST] Starting module execution: %s", name)

	h.mu.RLock()
	compiledModule, exists := h.cache[name]
	h.mu.RUnlock()

	if !exists {
		return nil, nil, fmt.Errorf("module %s not found in cache", name)
	}

	// Create pipes for stdio for this specific instance.
	hostToWasmR, hostToWasmW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	wasmToHostR, wasmToHostW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	log.Printf("[WASM HOST] Created pipes for module %s", name)

	// Step 1: Configure the WASI system with all extensions
	builder := imports.NewBuilder().
		WithNonBlockingStdio(true).
		WithSocketsExtension("wasmedgev2", compiledModule).
		WithDirs("/").
		WithStdio(int(hostToWasmR.Fd()), int(wasmToHostW.Fd()), int(os.Stderr.Fd())).
		WithEnv(envVars...)

	log.Printf("[WASM HOST] Configured WASI builder for module %s with %d env vars", name, len(envVars))

	// Step 2: Instantiate the WASI system
	ctx, _, err = builder.Instantiate(ctx, h.runtime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI system: %w", err)
	}

	log.Printf("[WASM HOST] Instantiated WASI system for module %s", name)

	// Step 3: Instantiate WASI HTTP v1 extension with logging
	wasiHTTP := wasi_http.MakeWasiHTTP()
	if err := wasiHTTP.Instantiate(ctx, h.runtime); err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI HTTP: %v", err)
	}

	log.Printf("[WASM HOST] Instantiated WASI HTTP for module %s", name)

	// Step 4: Instantiate and run the WASM module
	go func() {
		log.Printf("[WASM HOST] Starting WASM module execution in goroutine: %s", name)
		wazeroConfig := wazero.NewModuleConfig()
		if _, err = h.runtime.InstantiateModule(ctx, compiledModule, wazeroConfig); err != nil {
			// Handle exit errors properly
			if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
				log.Printf("[WASM HOST] Module %s exited with code: %d", name, exitErr.ExitCode())
			} else {
				log.Printf("[WASM HOST] Failed to instantiate module %s: %v", name, err)
			}
		} else {
			log.Printf("[WASM HOST] Module %s executed successfully", name)
		}
	}()

	log.Printf("[WASM HOST] Module %s setup completed, returning pipes", name)

	// Return wrapped pipes that log all I/O
	return &loggingReader{wasmToHostR, name}, &loggingWriter{hostToWasmW, name}, nil
}

// LoadModule compiles a WASM module from its bytes and caches it for future runs.
func (h *WasmHost) LoadModule(ctx context.Context, name string, wasmBytes []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.cache[name]; exists {
		return fmt.Errorf("module %s is already loaded", name)
	}

	compiledModule, err := h.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile wasm module %s: %w", name, err)
	}

	h.cache[name] = compiledModule
	log.Printf("Compiled and cached WASM module: %s", name)
	return nil
}

// RunModule instantiates a pre-compiled module from the cache and runs it.
func (h *WasmHost) RunModule(ctx context.Context, name string) (io.Reader, io.Writer, error) {
	h.mu.RLock()
	compiledModule, exists := h.cache[name]
	h.mu.RUnlock()

	if !exists {
		return nil, nil, fmt.Errorf("module %s not found in cache", name)
	}

	// Create pipes for stdio for this specific instance.
	hostToWasmR, hostToWasmW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	wasmToHostR, wasmToHostW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Step 1: Configure the WASI system with all extensions
	builder := imports.NewBuilder().
		WithNonBlockingStdio(true).
		WithSocketsExtension("wasmedgev2", compiledModule).
		WithDirs("/").
		WithStdio(int(hostToWasmR.Fd()), int(wasmToHostW.Fd()), int(os.Stderr.Fd()))

	// Step 2: Instantiate the WASI system
	ctx, _, err = builder.Instantiate(ctx, h.runtime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI system: %w", err)
	}
	// Step 4: Instantiate WASI HTTP v1 extension
	wasiHTTP := wasi_http.MakeWasiHTTP()
	if err := wasiHTTP.Instantiate(ctx, h.runtime); err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI HTTP: %v", err)
	}
	// Step 5: Instantiate and run the WASM module
	go func() {
		wazeroConfig := wazero.NewModuleConfig()
		if _, err = h.runtime.InstantiateModule(ctx, compiledModule, wazeroConfig); err != nil {
			// Handle exit errors properly
			if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
				log.Printf("exit_code: %d\n", exitErr.ExitCode())
			} else {
				log.Printf("failed to instantiate wasm module: %v", err)
			}
		}
	}()
	// Return the communication pipe
	return wasmToHostR, hostToWasmW, nil
}

// Close cleanly shuts down the wazero runtime.
func (h *WasmHost) Close(ctx context.Context) error {
	return h.runtime.Close(ctx)
}

// wasmPipeComm wraps pipes for WASM communication
// Host writes to stdinW, reads from stdoutR
type wasmPipeComm struct {
	stdinW  *os.File // host writes to this (WASM stdin)
	stdoutR *os.File // host reads from this (WASM stdout)
}

func (w *wasmPipeComm) Read(p []byte) (n int, err error) {
	return w.stdoutR.Read(p)
}

func (w *wasmPipeComm) Write(p []byte) (n int, err error) {
	return w.stdinW.Write(p)
}

func (w *wasmPipeComm) Close() error {
	err1 := w.stdinW.Close()
	err2 := w.stdoutR.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// loggingReader wraps an io.Reader to log all read operations
type loggingReader struct {
	reader io.Reader
	module string
}

func (l *loggingReader) Read(p []byte) (n int, err error) {
	n, err = l.reader.Read(p)
	if n > 0 {
		log.Printf("[WASM I/O] Module %s read %d bytes: %q", l.module, n, string(p[:n]))
	}
	if err != nil && err != io.EOF {
		log.Printf("[WASM I/O] Module %s read error: %v", l.module, err)
	}
	return n, err
}

// loggingWriter wraps an io.Writer to log all write operations
type loggingWriter struct {
	writer io.Writer
	module string
}

func (l *loggingWriter) Write(p []byte) (n int, err error) {
	log.Printf("[WASM I/O] Writing %d bytes to module %s: %q", len(p), l.module, string(p))
	n, err = l.writer.Write(p)
	if err != nil {
		log.Printf("[WASM I/O] Module %s write error: %v", l.module, err)
	}
	return n, err
}

// RunWasm compiles and runs a WASM module in a single call.
// Deprecated: This is inefficient for repeated calls. Use a WasmHost to cache compiled modules instead.
func RunWasm(ctx context.Context, wasmBytes []byte) (io.Reader, io.Writer, error) {
	// Create pipes for stdio
	hostToWasmR, hostToWasmW, err := os.Pipe() // host writes, wasm reads (stdin)
	if err != nil {
		log.Fatalf("failed to create stdin pipe: %v", err)
	}
	wasmToHostR, wasmToHostW, err := os.Pipe() // wasm writes, host reads (stdout)
	if err != nil {
		log.Fatalf("failed to create stdout pipe: %v", err)
	}

	// Create a new WebAssembly Runtime
	runtime := wazero.NewRuntime(ctx)
	// defer runtime.Close(ctx)
	// Compile the WASM module
	wasmModule, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile wasm module: %v", err)
	}
	// defer wasmModule.Close(ctx)
	// Step 2: Configure the WASI system with all extensions
	builder := imports.NewBuilder().
		WithNonBlockingStdio(true).
		WithSocketsExtension("wasmedgev2", wasmModule).
		WithDirs("/").
		WithStdio(int(hostToWasmR.Fd()), int(wasmToHostW.Fd()), int(os.Stderr.Fd()))
	// Step 3: Instantiate the WASI system

	wasiSystem, _, err := builder.Instantiate(ctx, runtime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI system: %v", err)
	}
	_ = wasiSystem // Store if needed for cleanup

	// Step 4: Instantiate WASI HTTP v1 extension
	wasiHTTP := wasi_http.MakeWasiHTTP()
	if err := wasiHTTP.Instantiate(ctx, runtime); err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI HTTP: %v", err)
	}
	// Step 5: Instantiate and run the WASM module
	go func() {
		wazeroConfig := wazero.NewModuleConfig().WithStdin(hostToWasmR).WithStdout(wasmToHostW)
		if _, err = runtime.InstantiateModule(ctx, wasmModule, wazeroConfig); err != nil {
			// Handle exit errors properly
			if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
				log.Printf("exit_code: %d\n", exitErr.ExitCode())
			} else {
				log.Printf("failed to instantiate wasm module: %v", err)
			}
		}
	}()
	// Return the communication pipe
	return wasmToHostR, hostToWasmW, nil
}

func RunWasm_old(ctx context.Context, wasmBytes []byte) (io.Reader, io.Writer, error) {
	// Create pipes for stdio
	hostToWasmR, hostToWasmW, err := os.Pipe() // host writes, wasm reads (stdin)
	if err != nil {
		log.Fatalf("failed to create stdin pipe: %v", err)
	}
	wasmToHostR, wasmToHostW, err := os.Pipe() // wasm writes, host reads (stdout)
	if err != nil {
		log.Fatalf("failed to create stdout pipe: %v", err)
	}

	// Create a new WebAssembly Runtime
	runtime := wazero.NewRuntime(ctx)
	// defer runtime.Close(ctx)
	// Compile the WASM module
	wasmModule, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile wasm module: %v", err)
	}
	// defer wasmModule.Close(ctx)
	// Step 2: Configure the WASI system with all extensions
	builder := imports.NewBuilder().
		WithNonBlockingStdio(true).
		WithSocketsExtension("wasmedgev2", wasmModule).
		WithDirs("/").
		WithStdio(int(hostToWasmR.Fd()), int(wasmToHostW.Fd()), int(os.Stderr.Fd()))
	// Step 3: Instantiate the WASI system

	wasiSystem, _, err := builder.Instantiate(ctx, runtime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI system: %v", err)
	}
	_ = wasiSystem // Store if needed for cleanup
	// defer system.Close(ctx)

	// Step 4: Instantiate WASI HTTP v1 extension
	wasiHTTP := wasi_http.MakeWasiHTTP()
	if err := wasiHTTP.Instantiate(ctx, runtime); err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASI HTTP: %v", err)
	}
	// Step 5: Instantiate and run the WASM module
	go func() {
		wazeroConfig := wazero.NewModuleConfig().WithStdin(hostToWasmR).WithStdout(wasmToHostW)
		if _, err = runtime.InstantiateModule(ctx, wasmModule, wazeroConfig); err != nil {
			// Handle exit errors properly
			if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
				log.Printf("exit_code: %d\n", exitErr.ExitCode())
			} else {
				log.Printf("failed to instantiate wasm module: %v", err)
			}
		}
	}()
	// Return the communication pipe
	return wasmToHostR, hostToWasmW, nil
}
