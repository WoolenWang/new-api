FRONTEND_DIR = ./web
BACKEND_DIR = .

.PHONY: all build-frontend start-backend build

all: build-frontend start-backend

build-frontend:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat VERSION) bun run build

start-backend:
	@echo "Starting backend dev server..."
	@cd $(BACKEND_DIR) && go run main.go &

build:
	@echo "Starting build backend server..."
	@cd $(BACKEND_DIR) && FRONTEND_BASE_URL="/static" go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api

build_admin:
	@echo "Starting build backend server..."
	@cd $(BACKEND_DIR) && go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=admin'" -o new-api-admin
