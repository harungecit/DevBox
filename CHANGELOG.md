# Changelog

All notable changes to DevBox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and versions follow SemVer.

## [0.3.0] — 2026-08-29

### Added
- **HTTPS at the front-door.** The built-in proxy now terminates TLS on :443 with your mkcert certificates, so `https://myapp.test` works for every backend — nginx, Apache, Caddy, FrankenPHP and dev servers (Next.js, Django, Go…). Plain HTTP requests redirect to HTTPS; tunnel traffic is passed through untouched.
- **"Backend not running" page.** When a dev server or web server behind a domain is stopped, the front-door shows a friendly page that refreshes itself instead of a bare 502.
- **Dev-server keep-alive.** A per-project **AUTO** toggle starts the dev server with DevBox and restarts it after a crash (max 3 times in 5 minutes). Crashes are reported in the UI with the last meaningful log line.
- **Framework catalog.** Import detection now recognises 40+ frameworks (Laravel, Lumen, Symfony, CodeIgniter, Yii, CakePHP, Slim, Laminas, Drupal, WordPress, Joomla, Magento, PrestaShop, Next.js, Nuxt, NestJS, Astro, Remix, SvelteKit, Angular, Gatsby, Vue, React, Svelte, AdonisJS, Express, Fastify, Koa, Hono, Django, Flask, FastAPI, Goravel, Gin, Fiber, Echo, Actix, Axum, Rocket…) with the right start command and default port for each.
- **New scaffold templates:** CodeIgniter, Slim, CakePHP, Yii, NestJS, Astro, SvelteKit, Express, Gin, Flask, FastAPI.
- **Developer tools per runtime** on the Tools page: uv, pipx, Poetry (Python); air, golangci-lint, gopls (Go); cargo-watch, cargo-edit, cargo-audit (Rust). Installed under `C:\DevBox\tools` and added to PATH.
- **Service management UIs:** Redis Commander (Redis/Valkey) and mongo-express (MongoDB) — installed with one click, opened against the DevBox instance.
- **Editable connection settings** in the service info panel: port, user, password and (Redis/Valkey) database count. Changes are applied to the real server (PostgreSQL `ALTER ROLE` + `pg_hba.conf`, MySQL/MariaDB `ALTER USER`, Redis `requirepass`) and reflected in URIs, CLI hints and tools.
- **Terminal integration.** Open your own terminal (Windows Terminal, PowerShell 7, Git Bash, Cmder, ConEmu, cmd; iTerm2/Terminal on macOS) from a project — its pinned runtime first on PATH — from a service — `psql`/`mysql`/`redis-cli`/`mongosh` already connected — or globally from the Dashboard or tray. Preferred terminal is selectable in Settings.
- **Confirmation dialogs** before removing a project or uninstalling a tool, matching services.
- `CHANGELOG.md`.

### Changed
- README rewritten for users: what DevBox is, what it installs, how to get started, FAQ.
- Importing a Node/Python/Go/Rust project now brings up the front-door automatically so the `.test` domain works immediately; provisioning problems (declined UAC prompt, proxy failure) are shown in the UI instead of being logged silently.
- The runtime selector is locked to the detected framework (no more "PHP" for a Next.js app).
- Project row layout no longer wraps domain/port/status when the path is long; the domain is clickable.
- Tools whose runtime or service is missing show "Install X →" instead of an Install button.
- Removing a project also deletes its mkcert certificate files.

### Fixed
- **Redis and Valkey did not start on Windows** — the MSYS-based builds cannot read `C:\…` config paths; paths are now relative to the service directory.
- **Stopping left orphan processes** (npm `.cmd` shims, nginx/apache workers): every stop now kills the whole process tree, and npm-based web tools run through `node` directly.
- Domain names are sanitised on the backend too (`Personal Project.Manager` → `personal-project-manager.test`).
- Hosts-file "already present" check matched substrings (`app.test` vs `my-app.test`).
- Dev-server crash notices no longer show ANSI escape codes.

## [0.2.1] — 2026-08-28

See the [GitHub release](https://github.com/harungecit/DevBox/releases/tag/v0.2.1).
