module github.com/joshcarp/mcper

go 1.23.0

toolchain go1.24.4

require (
	github.com/breml/rootcerts v0.3.0
	github.com/modelcontextprotocol/go-sdk v0.1.0
	github.com/spf13/cobra v1.8.0
	github.com/stealthrocket/net v0.2.1
	github.com/stealthrocket/wasi-go v0.8.0
	github.com/tetratelabs/wazero v1.9.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stealthrocket/wazergo v0.19.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

replace github.com/modelcontextprotocol/go-sdk => github.com/joshcarp/go-sdk v0.0.0-20250710224607-e052a06d1858
