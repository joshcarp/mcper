// MCPER Chrome Bridge - Native Messaging Host
//
// This binary provides an HTTP server that bridges between the MCPER WASM plugin
// and the Chrome extension. It communicates with the extension via Chrome's
// native messaging protocol.
//
// Usage:
//   chrome-bridge                    # Run as native messaging host (stdin/stdout)
//   chrome-bridge -server            # Run as HTTP server on port 9223
//   chrome-bridge -server -port 8080 # Run on custom port

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Message represents a command/response message
type Message struct {
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Success bool            `json:"success,omitempty"`
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// Bridge handles communication between HTTP and native messaging
type Bridge struct {
	mu              sync.Mutex
	pendingRequests map[string]chan Message
	requestID       int
	nativeWriter    io.Writer
	nativeReader    io.Reader
}

func NewBridge() *Bridge {
	return &Bridge{
		pendingRequests: make(map[string]chan Message),
	}
}

func main() {
	serverMode := flag.Bool("server", false, "Run as HTTP server instead of native messaging host")
	port := flag.Int("port", 9223, "HTTP server port (only used with -server)")
	flag.Parse()

	if *serverMode {
		runHTTPServer(*port)
	} else {
		runNativeHost()
	}
}

// runNativeHost runs in native messaging mode (stdin/stdout communication)
func runNativeHost() {
	log.SetOutput(os.Stderr) // Log to stderr to keep stdout clean for messages

	bridge := NewBridge()
	bridge.nativeWriter = os.Stdout
	bridge.nativeReader = os.Stdin

	// Read messages from extension
	go bridge.readNativeMessages()

	// Keep running until stdin closes
	select {}
}

// runHTTPServer runs the HTTP server that the WASM plugin calls
func runHTTPServer(port int) {
	bridge := NewBridge()

	// Set up native messaging if running alongside extension
	// For standalone mode, we'll simulate responses
	bridge.nativeWriter = os.Stdout
	bridge.nativeReader = os.Stdin

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Command endpoints
	mux.HandleFunc("/navigate", bridge.handleCommand("navigate"))
	mux.HandleFunc("/click", bridge.handleCommand("click"))
	mux.HandleFunc("/type", bridge.handleCommand("type"))
	mux.HandleFunc("/screenshot", bridge.handleCommand("screenshot"))
	mux.HandleFunc("/html", bridge.handleCommand("get_html"))
	mux.HandleFunc("/text", bridge.handleCommand("get_text"))
	mux.HandleFunc("/evaluate", bridge.handleCommand("evaluate"))
	mux.HandleFunc("/tabs", bridge.handleCommand("list_tabs"))
	mux.HandleFunc("/tabs/switch", bridge.handleCommand("switch_tab"))
	mux.HandleFunc("/tabs/new", bridge.handleCommand("new_tab"))
	mux.HandleFunc("/tabs/close", bridge.handleCommand("close_tab"))
	mux.HandleFunc("/scroll", bridge.handleCommand("scroll"))
	mux.HandleFunc("/wait", bridge.handleCommand("wait"))
	mux.HandleFunc("/cdp", bridge.handleCommand("cdp"))

	// Generic command endpoint
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result, err := bridge.sendCommand(msg.Command, msg.Params)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// CORS middleware
	handler := corsMiddleware(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("MCPER Chrome Bridge HTTP server starting on port %d", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) handleCommand(command string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params json.RawMessage

		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(body) > 0 {
				params = body
			}
		} else if r.Method == http.MethodGet {
			// Convert query params to JSON
			queryParams := make(map[string]string)
			for key, values := range r.URL.Query() {
				if len(values) > 0 {
					queryParams[key] = values[0]
				}
			}
			if len(queryParams) > 0 {
				params, _ = json.Marshal(queryParams)
			}
		}

		result, err := b.sendCommand(command, params)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func (b *Bridge) sendCommand(command string, params json.RawMessage) (Message, error) {
	b.mu.Lock()
	b.requestID++
	id := fmt.Sprintf("req-%d", b.requestID)
	responseChan := make(chan Message, 1)
	b.pendingRequests[id] = responseChan
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pendingRequests, id)
		b.mu.Unlock()
	}()

	msg := Message{
		ID:      id,
		Command: command,
		Params:  params,
	}

	if err := b.writeNativeMessage(msg); err != nil {
		return Message{}, fmt.Errorf("failed to send command: %w", err)
	}

	// Wait for response with timeout
	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(30 * time.Second):
		return Message{}, fmt.Errorf("command timeout")
	}
}

func (b *Bridge) readNativeMessages() {
	reader := bufio.NewReader(b.nativeReader)

	for {
		// Read message length (4 bytes, little-endian)
		var length uint32
		if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
			if err == io.EOF {
				log.Println("Native messaging connection closed")
				return
			}
			log.Printf("Error reading message length: %v", err)
			continue
		}

		// Sanity check on length
		if length > 1024*1024 {
			log.Printf("Message too large: %d bytes", length)
			continue
		}

		// Read message content
		content := make([]byte, length)
		if _, err := io.ReadFull(reader, content); err != nil {
			log.Printf("Error reading message content: %v", err)
			continue
		}

		var msg Message
		if err := json.Unmarshal(content, &msg); err != nil {
			log.Printf("Error parsing message: %v", err)
			continue
		}

		// Route response to waiting request
		if msg.ID != "" {
			b.mu.Lock()
			if ch, ok := b.pendingRequests[msg.ID]; ok {
				ch <- msg
			}
			b.mu.Unlock()
		}
	}
}

func (b *Bridge) writeNativeMessage(msg Message) error {
	content, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Write length prefix (4 bytes, little-endian)
	length := uint32(len(content))
	if err := binary.Write(b.nativeWriter, binary.LittleEndian, length); err != nil {
		return err
	}

	// Write content
	_, err = b.nativeWriter.Write(content)
	return err
}
