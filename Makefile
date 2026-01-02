all: build

build:
	go build -o ./bin/mcper ./cmd/mcper/

wasm-build:
	GOOS=wasip1 GOARCH=wasm go build -o ./wasm/plugin-hello.wasm ./plugins/hello/
	GOOS=wasip1 GOARCH=wasm go build -o ./wasm/plugin-linkedin.wasm ./plugins/linkedin/
	GOOS=wasip1 GOARCH=wasm go build -o ./wasm/plugin-gmail.wasm ./plugins/gmail/
	GOOS=wasip1 GOARCH=wasm go build -o ./wasm/plugin-github.wasm ./plugins/github/
	GOOS=wasip1 GOARCH=wasm go build -o ./wasm/plugin-azuredevops.wasm ./plugins/azuredevops/

test:
	go test -timeout 30s -count=1 -v -cover ./...

test-coverage:
	go test -timeout 30s -count=1 -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out | tail -1

clean:
	rm -rf ./bin ./wasm/*.wasm ./dist
