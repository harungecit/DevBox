# Changelog

All notable changes to DevBox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and versions follow SemVer.

## [0.4.1] — 2026-08-31

### Fixed
- **In-app update could vanish without a trace.** The silent updater quit DevBox blindly 1.5 s after launching the installer; if the installer then failed on that machine (antivirus holding the executable, `taskkill` blocked, custom install directory), nothing was ever shown — "UAC appeared, DevBox closed, and that was it". Now:
  - DevBox **waits on the installer** and, if it exits without completing, stays open and shows the failure with its exit code.
  - The installer writes a step-by-step log to `%TEMP%\DevBox-update.log` (in the elevating admin's TEMP), so a failed update finally leaves evidence.
  - An update that cannot close DevBox **aborts cleanly** (exit code 5) instead of continuing into a broken half-install.
  - The install directory is remembered (`InstallLocation` + `InstallDirRegKey`) and the updater passes `/D=` with its own location — **custom install directories now update in place** instead of installing a second copy to Program Files.
  - App relaunch after a silent update falls back to a direct start if the `explorer.exe` handoff fails.
- Users on **0.4.0 or older** still carry the old updater — this version needs to be installed manually once; updates after that are covered.

## [0.4.0] — 2026-08-31

### Added
- **Import Center.** DevBox now scans your machine for runtimes (Go, Node.js, PHP, Python, Rust) and services (Nginx, Apache, Caddy, PostgreSQL, MySQL, MariaDB, MongoDB, Redis, Mailpit) installed outside DevBox — PATH, Program Files, nvm, scoop, XAMPP, Laragon, WAMP, Homebrew — and imports them **in place**: the installation is linked (NTFS junction / symlink), never moved or copied. Imported versions behave exactly like DevBox installs — global switch, PATH, per-project pinning, in-place updates — and removing one only removes the link. Imported services get a DevBox-managed configuration and a fresh data directory on a free port; the original installation and its data stay untouched. Reachable via the "Import from system" button on the Runtimes and Services pages.

### Changed
- **MySQL and MariaDB — and Redis and Valkey — can now be installed side by side** on different ports; the "one engine per group" restriction is gone (web servers stay exclusive — vhost generation assumes a single active one). Each installed engine gets its own row on the Services page, and installs/imports pick a free port automatically (MariaDB next to MySQL lands on 3307).
- Dashboard header: the Terminal button moved next to Refresh and now uses the primary color.

### Fixed
- **Progress bar could freeze at 100% and lock the page** after fast operations (like link-based imports): the final installed/error event could overtake the last progress events and the cleared state was resurrected. Event ordering is now guaranteed for runtime installs/updates/imports, service jobs, PECL extensions, Composer, scaffolding and cloning.

## [0.3.2] — 2026-08-29

### Fixed
- **In-app update now completes on its own.** The installer closes a running DevBox itself (no more "Can't write DevBox.exe"), runs silently when started from the app, and relaunches DevBox as the normal user afterwards. The finish page of a manual install also offers "Start DevBox".

## [0.3.1] — 2026-08-29

### Fixed
- **In-app update did not install.** The downloaded installer needs elevation (it targets Program Files) and was launched without it, so Windows refused silently. It is now started through the shell with a UAC prompt; DevBox quits right after so the installer can replace its files.

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
