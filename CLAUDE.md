# New API Project Context

## Project Overview
**New API** is a comprehensive AI model gateway and asset management system, forked from [One API](https://github.com/songquanpeng/one-api). It serves as a unified interface for accessing various Large Language Model (LLM) providers (OpenAI, Azure, Anthropic Claude, Google Gemini, Midjourney, etc.), offering features like:
- **Unified API:** Standardizes different LLM APIs into an OpenAI-compatible format.
- **Billing & Quotas:** Multi-user management, token/request-based billing, and integration with payment providers (Stripe, Epay).
- **Channel Management:** Routing, load balancing, and automatic failover for different API keys/channels.
- **Security:** Rate limiting, user authentication (OIDC, OAuth2), and detailed logging.

## Tech Stack
- **Backend:** Go (Golang) v1.25+
- **Web Framework:** Gin (HTTP server)
- **Database (ORM):** GORM (Supports SQLite, MySQL, PostgreSQL)
- **Caching:** Redis (optional but recommended for production)
- **Frontend:** React (Vite build tool), styled with Tailwind CSS (inferred).
- **Containerization:** Docker & Docker Compose

## Project Structure
*   `main.go`: Application entry point. Initializes resources, middleware, and starts the HTTP server.
*   `common/`: Shared utilities, constants, and global configuration variables (e.g., `env.go`, `gin.go`).
*   `controller/`: HTTP request handlers. Contains business logic for users, billing, and API management.
*   `middleware/`: Gin middleware for authentication, CORS, rate limiting, logging, etc.
*   `model/`: Database models and data access layer.
*   `relay/`: Core logic for proxying and adapting requests to different LLM providers.
*   `router/`: API route definitions.
*   `web/`: Frontend source code (React + Vite).
*   `bin/`: Utility scripts (migration, testing).
*   `docker-compose.yml`: Configuration for running the service via Docker.
*   `makefile`: Automation for building frontend and starting backend.

## Setup & Development

### Prerequisites
- **Go:** Version 1.18+ (Project uses 1.25)
- **Node.js / Bun:** For building the frontend.
- **Docker:** (Optional) For containerized deployment.
- **Redis:** (Optional) For caching.

### Running Locally

**1. Backend:**
```bash
# Copy example env file
cp .env.example .env

# Install dependencies & Run
go mod download
go run main.go
```
*The server runs on port 3000 by default (or defined in `PORT` env var).*

**2. Frontend:**
```bash
cd web
bun install
bun run build # Or 'bun run dev' for development server
```

**3. Using Makefile:**
The project includes a `makefile` to streamline tasks:
```bash
make all            # Builds frontend and starts backend
make build-frontend # Only builds the React app
make start-backend  # Only starts the Go server
```

### Docker Deployment
The recommended way for production deployment is via Docker Compose:
```bash
docker-compose up -d
```
*Ensure `docker-compose.yml` is configured correctly, especially `SQL_DSN` if using an external database.*

## Key Configuration (Environment Variables)
Configuration is primarily handled via environment variables or the `.env` file.
- `PORT`: Server listening port (default: 3000).
- `SQL_DSN`: Database connection string (e.g., `root:123456@tcp(localhost:3306)/oneapi`).
- `REDIS_CONN_STRING`: Redis connection string.
- `SESSION_SECRET`: Secret for session encryption.
- `CRYPTO_SECRET`: Secret for encrypting sensitive data in DB.
- `gin_mode`: Set to `release` for production.

## Coding Conventions
- **Controllers:** Grouped by functionality (e.g., `user.go`, `channel.go`).
- **Models:** Define struct tags for JSON and GORM.
- **Error Handling:** Use `common.FatalLog` or `common.SysLog` for server-side logging. HTTP errors are returned as JSON.
- **Formatting:** Standard `gofmt` / `goimports`.
