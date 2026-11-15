.PHONY: all build clear-build daemon status tray install test \
        on off refresh uninstall reinstall deps

# Binary name
BINARY_NAME=network-router
BUILD_DIR=build

all: build

build:
	@echo "Building..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) main.go

clear-build:
	@echo "Clearing..."
	rm -rf $(BUILD_DIR)

# Daemon commands
daemon: build
	@echo "Starting daemon..."
	sudo ./$(BUILD_DIR)/$(BINARY_NAME) daemon -config ./config.yaml

tray: build
	@echo "Starting tray application..."
	./$(BUILD_DIR)/$(BINARY_NAME) tray

status:
	@./$(BUILD_DIR)/$(BINARY_NAME) status

on:
	@./$(BUILD_DIR)/$(BINARY_NAME) on

off:
	@./$(BUILD_DIR)/$(BINARY_NAME) off

refresh:
	@./$(BUILD_DIR)/$(BINARY_NAME) refresh

# Tests
test:
	@echo "Running tests..."
	go vet ./...
	go test -race -count=1 ./...

# Install service
install: build
	@echo "Installing service..."
	sudo ./install_service.sh

uninstall:
	@echo "Uninstalling service..."
	sudo ./uninstall_service.sh

reinstall: uninstall install

deps:
	@echo "Tidying dependencies..."
	go mod tidy
