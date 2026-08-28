<p align="center">
  <img src="build/appicon.png" width="96" alt="DevBox">
</p>
<h1 align="center">DevBox</h1>
<p align="center"><b>Your whole local development stack, one click away.</b><br>
Runtimes · services · <code>.test</code> domains · SSL · Cloudflare tunnels — in a single desktop app.</p>

<p align="center">
  <a href="https://github.com/harungecit/DevBox/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/harungecit/DevBox?display_name=tag&color=08a6d0"></a>
  <a href="https://github.com/harungecit/DevBox/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/harungecit/DevBox/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="MIT" src="https://img.shields.io/badge/license-MIT-green"></a>
  <img alt="Platform" src="https://img.shields.io/badge/Windows-10%2F11%20x64-0078d4">
  <img alt="macOS" src="https://img.shields.io/badge/macOS-coming%20soon-lightgrey">
</p>

<p align="center">
  <a href="https://devbox.harungecit.dev/">Website</a> ·
  <a href="https://github.com/harungecit/DevBox/releases/latest">Download</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#features">Features</a> ·
  <a href="#building-from-source">Build from source</a> ·
  <a href="https://devbox.harungecit.dev/privacy.html">Privacy</a>
</p>

---

DevBox is a free, open-source (MIT) desktop application for developers who would rather ship than configure. It downloads official builds of **Go, Node.js, PHP, Python and Rust**, installs and supervises **Nginx, Apache, Caddy, FrankenPHP, PostgreSQL, MySQL, MariaDB, MongoDB, Redis and Mailpit**, gives every project a **`myapp.test` domain with a trusted local certificate**, and can **share any project to the internet through Cloudflare** — on your own domain if you like. No Docker, no PATH surgery, no telemetry. Everything lives in one folder: `C:\DevBox`.

## Why DevBox?

| Pain | DevBox |
|---|---|
| “Which PHP/Node version is this project on?” | Any number of versions side by side; one global, others pinned per project. Each PHP version gets its own FastCGI instance automatically. |
| A new patch release means a fresh install and lost config | Minor/patch releases **update in place** — php.ini, Composer, yarn/pnpm, global npm & pip packages carried over. |
| Ports, vhosts, hosts-file edits | A built-in front-door proxy routes `*.test` to the right backend (nginx, FrankenPHP or your dev server). Hosts entries are written for you (one UAC prompt). |
| “It works on my machine” demos | One click → public URL via Cloudflare. Link your account → `app.yourdomain.com` with DNS handled. |
| Hunting for extensions and tools | Dev-tuned php.ini with common extensions on, PECL extensions (Xdebug, Redis, Imagick, MongoDB…) one click away, Composer, npm/yarn/pnpm/Bun, Adminer, mkcert, cloudflared. |
| Background daemons and hidden folders | Runs in the tray, keeps services up until you quit, and stores everything in `C:\DevBox`. |

## Features

**Runtimes** — Go · Node.js · PHP · Python · Rust
- Install any version from the official sources (checksums verified). Version lists are cached and refreshed in the background.
- Global version = on your PATH, powers FastCGI, used by unpinned projects. Pin a different version per project.
- In-place updates within a release line with settings migration; installed-count badges; one-click npm self-update.

**Services** — Nginx · Apache · Caddy · FrankenPHP · PostgreSQL · MySQL · MariaDB · MongoDB · Redis · Mailpit
- Pick a version and a port (conflicts detected before install), start/stop/restart, auto-start with DevBox, logs and connection details in the UI.
- In-place updates that keep your data, configuration and vhosts. Major upgrades of databases are flagged, not forced.

**Projects**
- Add a folder, scaffold from a template (Laravel, Symfony, WordPress, Next.js, Nuxt, Vue, React, Svelte, Angular, Go, Rust, Django, static) or clone a Git repo.
- Framework detection, `.test` domain, SSL via mkcert, per-project runtime/version/web-server choice, dev-server start/stop with logs.
- Share: quick Cloudflare tunnel or a custom hostname on your own zone.

**Quality of life**
- English & Turkish UI, light/dark/system theme, start at login (minimized), close to tray, in-app update check via GitHub Releases.

## Quick start

1. Download **`DevBox-Setup-<version>-windows-amd64.exe`** from the [latest release](https://github.com/harungecit/DevBox/releases/latest) and run it.
   If Windows SmartScreen appears, choose **More info → Run anyway** (see [Security & signing](#security--signing)).
2. **Runtimes** → install PHP 8.4 (or Node, Go, …). The first version becomes global.
3. **Services** → install Nginx and PostgreSQL/MySQL; switch on **AUTO** so they start with DevBox.
4. **Overview** → Front-door → Install → Start (binds port 80 so URLs need no port).
5. **Projects** → Add → pick a folder or template → click the globe icon to register `myapp.test`, flip **SSL** → open in the browser.
6. Click the share icon to get a public URL. For your own domain: **Settings → Cloudflare**.

Full guide: <https://devbox.harungecit.dev/#usage>

## Platform support

| Platform | Status |
|---|---|
| Windows 10 / 11 (x64) | ✅ Supported — installer and portable zip on every release |
| macOS (Apple Silicon / Intel) | 🚧 Coming soon — the code base compiles for macOS, but macOS download sources for several runtimes/services still need validation; it will ship as a separate release once it passes the same tests as Windows |
| Linux | Not planned for now |

## Security & signing

- Every release is built by the public [`release.yml`](.github/workflows/release.yml) workflow from the tagged source, after `go vet`, unit tests and `svelte-check` pass. `SHA256SUMS-windows.txt` is attached to each release.
- DevBox is not yet signed with a paid Authenticode certificate, so SmartScreen may show *“Windows protected your PC”* on first run. The workflow already supports signing: add `WINDOWS_CERT_PFX_BASE64` and `WINDOWS_CERT_PASSWORD` secrets and builds are signed automatically. SmartScreen reputation also accrues over time as more people install the same signed binary.
- Elevation is requested only for the hosts file and (first start) for the front-door proxy binding port 80; both show a UAC prompt.
- DevBox sends no telemetry. See the [privacy policy](https://devbox.harungecit.dev/privacy.html) for the full list of network connections.

## Where things live

```
C:\DevBox\
├─ runtimes\{go,node,php,python,rust}\<version>\
├─ services\{nginx,postgres,mysql,...}\        # binaries, data, logs, generated configs
├─ projects\                                   # default home for scaffolded / cloned projects
├─ tools\{bun,mkcert,cloudflared,...}\
├─ cache\                                      # remote version lists
├─ ssl\certs\                                  # mkcert certificates
├─ logs\                                       # debug.log, migration.log, per-service logs
├─ config.json · projects.json · tunnel-routes.json
```
Uninstalling DevBox leaves this folder untouched.

## Building from source

Requirements: Go 1.24+, Node.js 20+, [Wails CLI v2.11](https://wails.io/docs/gettingstarted/installation) (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0`), and NSIS for the installer (`choco install nsis`).

```bash
git clone https://github.com/harungecit/DevBox.git
cd DevBox
wails dev                 # hot-reloading dev build
wails build               # build/bin/DevBox.exe
wails build -nsis         # + build/bin/DevBox-amd64-installer.exe
```

Useful checks (what CI runs):

```bash
go vet ./... && go test ./...
GOOS=darwin go build ./...            # cross-compile check
cd frontend && npm ci && npm run check && npm run build
```

Architecture notes for contributors are in [`CLAUDE.md`](CLAUDE.md): Wails v2 (Go backend + Svelte/TypeScript frontend), a `platform` abstraction layer for OS specifics, plugin-style runtime/service managers, and event-driven async installs.

## Releasing

Tag and push — CI does the rest:

```bash
git tag v0.3.0 && git push origin v0.3.0
```

The **Release** workflow runs the test suite, builds the Windows installer and portable zip (signing them if the secrets exist), generates release notes and publishes the GitHub Release. Running DevBox instances notice the new version within seconds of their next launch and can install it from **Settings → Application updates**.

## Contributing

Issues and pull requests are welcome. Please open an issue first for larger changes so we can agree on the approach. By contributing you agree that your contributions are licensed under the MIT License.

## Acknowledgements

DevBox stands on the shoulders of [Wails](https://wails.io), [Svelte](https://svelte.dev), [Caddy](https://caddyserver.com), [mkcert](https://github.com/FiloSottile/mkcert), [cloudflared](https://github.com/cloudflare/cloudflared), [Mailpit](https://mailpit.axllent.org), [Adminer](https://www.adminer.org), [FrankenPHP](https://frankenphp.dev), the PHP for Windows team, and every runtime and database project it installs. Thank you.

## License

[MIT](LICENSE) © 2026 [Harun Geçit](https://harungecit.dev)
