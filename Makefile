# Define variables
GO_BIN = go
GO_BUILD_FLAGS = -v
BINARY_NAME = gh-pt
WRAPPER_NAME = ghpt
MAIN_PACKAGE = .
WRAPPER_PACKAGE = ./cmd/wrapper
OUTPUT_DIR = .

.PHONY: all build wrapper install-wrapper clean fmt lint tidy

all: build wrapper

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(OUTPUT_DIR)
	$(GO_BIN) build $(GO_BUILD_FLAGS) -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete. Binary located at $(OUTPUT_DIR)/$(BINARY_NAME)"

wrapper:
	@echo "Building $(WRAPPER_NAME) wrapper..."
	@mkdir -p $(OUTPUT_DIR)
	$(GO_BIN) build $(GO_BUILD_FLAGS) -o $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper $(WRAPPER_PACKAGE)
	@chmod 0100 $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper
	@echo "Build complete. Wrapper located at $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper"

# Install wrapper to temp directory for AI sandbox
install-wrapper: wrapper
	@PID=$$(ps -o ppid= -p $$$$); \
	WRAPPER_DIR=/tmp/ghpt-ai-wrapper-$$PID; \
	mkdir -p $$WRAPPER_DIR; \
	chmod 0644 $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper; \
	cp $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper $$WRAPPER_DIR/$(WRAPPER_NAME); \
	chmod 0100 $$WRAPPER_DIR/$(WRAPPER_NAME); \
	chmod 0100 $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper; \
	echo "Wrapper installed to $$WRAPPER_DIR/$(WRAPPER_NAME)"

clean:
	@echo "Cleaning up..."
	@rm -f $(OUTPUT_DIR)/$(BINARY_NAME) $(OUTPUT_DIR)/$(WRAPPER_NAME)-wrapper
	@$(GO_BIN) clean
	@echo "Cleanup complete."

fmt:
	@echo "Formatting Go code..."
	$(GO_BIN) fmt ./...

lint:
	@echo "Running linters"
	GOTOOLCHAIN=go1.27.1 golangci-lint run ./...

tidy:
	@echo "Tidying Go modules..."
	$(GO_BIN) mod tidy