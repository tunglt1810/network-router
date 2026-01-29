.PHONY: all build clear-build daemon status tray install test-icons

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

enable:
	@./$(BUILD_DIR)/$(BINARY_NAME) enable

disable:
	@./$(BUILD_DIR)/$(BINARY_NAME) disable

apply:
	@./$(BUILD_DIR)/$(BINARY_NAME) apply

clear:
	@./$(BUILD_DIR)/$(BINARY_NAME) clear

restart:
	@./$(BUILD_DIR)/$(BINARY_NAME) restart

# Test icons
test-icons:
	@echo "Testing SVG icon embedding..."
	@go run test_icon_svg.go

# Install service
install: build
	@echo "Installing service..."
	sudo ./install_service.sh

uninstall:
	@echo "Uninstalling service..."
	sudo ./uninstall_service.sh

reinstall: uninstall install

# Legacy commands (for backward compatibility)
start-routing: build
	@echo "Running..."
	sudo ./$(BUILD_DIR)/$(BINARY_NAME) daemon -config ./config.yaml

clear-routing: clear
	@echo "Running..."
	sudo ./$(BUILD_DIR)/$(BINARY_NAME) clear

deps:
	@echo "Tidying dependencies..."
	go mod tidy
