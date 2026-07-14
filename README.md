# Go Theory Prep Web App

A lightweight, high-performance Go application designed to parse, prepare, and deliver an interactive web interface for studying theory questions. The application features an automated, first-run data crawler that hydrates a local SQLite database, and can be compiled natively for both Windows and Linux environments.

---

## 🚀 Key Features

* **Dual-Mode Execution:**
  * **Local Client Mode (Default):** Binds to localhost (`127.0.0.1`), automatically detects your operating system, and launches your default web browser on startup.
  * **Headless Server Mode:** Binds to all interfaces (`0.0.0.0`) to run silently inside home labs, Docker containers, virtual machines, or remote servers.
* **Smart First-Run Hydration:** Automatically detects if the database is empty on first boot, running a controlled, rate-limited index crawl and image downloader before starting the UI.
* **Quiet by Default:** Keeps the terminal clean during daily use. Easily toggle verbose debugging with the `--verbose` flag.
* **CI/CD Powered:** Automated GitHub Actions pipeline that runs the test suite on push and cross-compiles release binaries for both Windows and Linux.
* **Fully Embedded Frontend:** The Go binary embeds all frontend assets natively, meaning you get a single, self-contained executable with zero runtime file dependencies.

---

## 🛠️ Getting Started

### Prerequisites

* [Go 1.22+](https://go.dev/dl/) (if running from source or developing)
* SQLite (managed automatically by the application via driver)

### Quick Start (From Source)

To fetch dependencies, run tests, and start the application in local development mode:

```bash
# Download dependencies
go mod download

# Run the test suite
go test -v ./...

# Run the application (Local Mode)
go run .
```
---
## ⚙️ Configuration & Flags

Customize how the application runs using command-line flags:
| Flag      	| Type   	| Default 	| Description                                                                                                 	|
|-----------	|--------	|---------	|-------------------------------------------------------------------------------------------------------------	|
| --local   	| bool   	| true    	| Binds to 127.0.0.1 and auto-opens your default browser. Set to false to bind to 0.0.0.0 for remote hosting. 	|
| --port    	| string 	| 8080    	| The TCP port the web server will listen on.                                                                 	|
| --verbose 	| bool   	| false   	| Enables detailed debug statements and live crawler progress prints.                                         	|


Execution Examples
1. Running locally (with a custom port)
```bash
./theory-app --port=9090
```

2. Deploying on a headless home lab server, VM, or Docker
```bash
./theory-app --local=false --port=80
```
3. Troubleshooting or initial database hydration
```bash
./theory-app --verbose
```

## ☁️ Cloud Development (GitHub Codespaces / Gitpod)

When running inside a headless cloud environment like GitHub Codespaces:

1. Run the application:

```bash
go run .
```
2. The browser opener will gracefully bypass execution (safely reporting that xdg-open is not found on headless Linux).

3. VS Code / Codespaces will automatically detect the port binding (8080) and prompt you with a notification toast. Click "Open in Browser" to access your forwarded port.
---
## 🏗️ Automated CI/CD Pipeline

The project includes a GitHub Actions workflow (`.github/workflows/go.yml`) that triggers on any push or pull request to the main or master branches:

* Sets up the Go environment.

* Caches dependencies to speed up future runs.

* Runs all unit and integration tests (go test).

* Cross-compiles the source into optimized, self-contained binaries for:

   * Linux: dist/theory-app-linux-amd64

   * Windows: dist/theory-app-windows-amd64.exe
---
## 🚀 Completed Architecture

* **Index Crawler:** Parses category listings dynamically (currently configured for C1) to harvest question page routes.
* **Deep Scraper:** Explores question endpoints, handles modern Hebrew web DOM typography, and accurately identifies the correct answer using data element attributes.
* **Asset Pipeline:** Intercepts, maps, and downloads question images natively to local storage to eliminate live dependencies on external web servers.
* **Storage Layer:** Fully normalized multi-table schema using SQLite to manage static data blocks and user test state maps natively.
* **Embedded Web Server:** Incorporates a Go `net/http` backend that serves API endpoints and embeds the entire production frontend assets natively into the single executable.

---

## 📁 Directory Structure

```text
israel-theory-scraper/
├── .github/
│   └── workflows/
│       └── go.yml           # CI/CD pipeline (Tests & Cross-Compilation)
├── data/                    # Generated automatically
│   ├── theory.db            # SQLite persistent database file
│   └── images/              # Downloaded question images (.png, .jpg)
├── frontend/
│   └── dist/                # Embedded static frontend assets
│       └── index.html       # Responsive, RTL-styled study dashboard
├── db.go                    # Database connections, schemas, and transactions
├── main.go                  # Main execution entrypoint and CLI flag router
├── scrapper.go              # Network crawl, deep scraping, and image download code
├── scrapper_test.go         # Unit tests simulating page parsing variants
├── server.go                # WebServer configuration and API endpoint handlers
├── webserver_test.go        # HTTP suite testing API routes and mock handlers
└── go.mod / go.sum          # Go dependency declarations
```