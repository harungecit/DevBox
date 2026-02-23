# GEMINI.md - DevBox Project Context

## Project Overview
**DevBox** is a developer productivity tool designed to manage development environments on Windows. It provides a unified desktop interface (built with **Wails v2**, **Go**, and **Svelte**) for installing, configuring, and managing multiple versions of common runtimes and essential development services.

### Core Features
- **Runtime Management:** Install and switch between different versions of **Go**, **Node.js**, and **PHP**.
- **Service Management:** One-click installation and control (Start/Stop/Restart) for services like **Nginx**, **PostgreSQL**, **MySQL**, **MongoDB**, **Redis**, and **Mailpit**.
- **Environment Control:** Automatic management of the Windows **PATH** environment variable to ensure active runtimes are globally accessible.
- **Project Tracking:** Tools to manage local development projects and their associated runtimes/services (inferred from `internal/project`).
- **Internationalization:** Supports both **English** and **Turkish**.

## Architecture & Technology Stack
- **Backend:** Go 1.24+ using the [Wails v2](https://wails.io/) framework.
  - `main.go`: Application entry point and Wails configuration.
  - `app.go`: Backend logic and bindings exposed to the frontend.
  - `internal/`: Core application logic (Config, i18n, Path management, Runtimes, Services).
- **Frontend:** Svelte with TypeScript, built using Vite.
  - `frontend/src/App.svelte`: Main UI structure with sidebar navigation.
  - `frontend/src/pages/`: UI for Dashboard, Projects, Runtimes, Services, and Settings.
  - `frontend/src/lib/stores/`: Frontend state management (using Svelte stores).
- **Configuration & Storage:** 
  - User data and configuration are stored in `~/.devbox/` by default.
  - Individual service configurations are stored in `~/.devbox/services/<service-name>/devbox-service.json`.

## Development Conventions

### Backend (Go)
- **Registry Pattern:** Both Runtimes and Services use a registry pattern (`Registry` map) for extensibility. New services or runtimes should implement their respective interfaces (`ServiceManager` or `RuntimeManager`).
- **Wails Bindings:** Methods intended for frontend use are defined on the `App` struct in `app.go`.
- **Error Handling:** Errors are logged to `~/.devbox/logs/debug.log` using the `debugLog` helper in `app.go`.

### Frontend (Svelte/TS)
- **Components:** Modular Svelte components located in `frontend/src/lib/components/`.
- **Internationalization:** Uses `svelte-i18n` on the frontend, synced with Go-side i18n logic.
- **Styling:** TailwindCSS is used for styling. Configuration is in `frontend/tailwind.config.js`.

## Key Commands

### Prerequisites
- [Go](https://go.dev/dl/) 1.24 or higher.
- [Node.js](https://nodejs.org/) & npm.
- [Wails CLI](https://wails.io/docs/gettingstarted/installation).

### Development
```powershell
# Run the application in development mode (with hot-reload)
wails dev
```

### Build
```powershell
# Build the production executable
wails build
```

## Directory Structure
- `internal/config/`: Configuration management (`config.json`).
- `internal/runtime/`: Logic for downloading and managing Go, Node, PHP.
- `internal/service/`: Management logic for databases and web servers.
- `internal/pathenv/`: Windows-specific PATH registry manipulation.
- `internal/i18n/`: Localization files and logic.
- `frontend/wailsjs/`: Auto-generated Go-to-JS bindings (do not edit manually).
