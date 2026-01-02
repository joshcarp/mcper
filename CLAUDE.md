# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build           # Build CLI to ./bin/mcper
make wasm-build      # Build all WASM plugins to ./wasm/
make test            # Run tests with coverage
make test-coverage   # Generate HTML coverage report
make clean           # Remove build artifacts
```

## Architecture

mcper is a zero-config MCP (Model Context Protocol) tool aggregator that runs plugins in WASM sandboxes.

### Core Components

**CLI (`cmd/mcper/`)**: Cobra-based CLI with commands for init, add, serve, enable, update, cache, plugin, and registry management.

**WASM Host (`pkg/wasmhost/host.go`)**: Manages wazero runtime for executing WASM plugins. Uses wasi-go for WASI system calls and WASI HTTP support. Key type is `WasmHost` which caches compiled modules and handles stdio pipes for MCP communication.

**Plugin System (`pkg/mcper/`)**:
- `plugin.go`: Parses plugin sources (local WASM files, GCS URLs, HTTP MCP servers)
- `config.go`: JSON config parsing for plugin configurations
- `cache.go`: Plugin caching with SHA256 integrity verification in `~/.mcper/cache`
- `script.go`: Generates `.mcper/start.sh` bootstrapping script

**WASM Plugins (`plugins/`)**: Each plugin is a standalone Go program compiled to WASI (`GOOS=wasip1 GOARCH=wasm`). Plugins use the MCP SDK to register tools and communicate via stdio. Example: `plugins/github/main.go` implements GitHub API tools.

### Plugin Types

1. **Local WASM** (`./path.wasm`): Direct file path
2. **Registry WASM** (`https://storage.googleapis.com/mcper-releases/{version}/plugin-{name}.wasm`): Downloaded and cached
3. **HTTP MCP** (`http://...`): Remote MCP server proxied through

### Data Flow

1. `mcper serve --config-json '...'` starts MCP server on stdin/stdout
2. For each plugin, `serve.go` loads WASM or connects to HTTP endpoint
3. Creates MCP client session to each plugin
4. Discovers tools via `ListTools`, registers them on the aggregating server
5. Tool calls are forwarded to appropriate plugin

### Key Dependencies

- `tetratelabs/wazero`: WebAssembly runtime
- `stealthrocket/wasi-go`: WASI implementation with HTTP support
- `modelcontextprotocol/go-sdk`: MCP protocol implementation (uses forked version)
- `spf13/cobra`: CLI framework

### File Locations

- Logs: `~/.mcper/mcper.log`
- Cache: `~/.mcper/cache/plugins/`
- Project config: `.mcper/start.sh` (self-bootstrapping script)
