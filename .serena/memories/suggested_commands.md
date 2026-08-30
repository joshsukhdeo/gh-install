# Suggested Commands

Available via the Makefile:
- **Build**: `make build` (or `make all`) - Outputs binary to current directory.
- **Format**: `make fmt` - Runs `go fmt ./...`.
- **Lint**: `make lint` - Runs `golangci-lint run ./...`.
- **Dependencies**: `make tidy` - Runs `go mod tidy`.
- **Clean**: `make clean` - Removes built binary.