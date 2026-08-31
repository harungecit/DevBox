# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Use GitHub's private
vulnerability reporting instead: go to the repository's **Security** tab →
**Report a vulnerability**. You will get a response as soon as possible, normally
within a few days.

Only the latest release is supported with security fixes.

## Threat model in one paragraph

DevBox is a *local* development tool. Everything it manages is intended for the
local machine only: databases, caches and web tools are bound to `127.0.0.1`
(PostgreSQL, MongoDB, Redis, Mailpit, php-cgi, Adminer and the other UIs
already are; restricting the remaining listeners — MySQL/MariaDB and the web
servers — to loopback is being rolled out). The deliberate exceptions: the
front-door proxy listens on ports 80/443 of the machine, and Cloudflare
tunnels expose a project to the internet **only** when you click share.
Elevation (UAC / administrator prompt) is requested solely for writing the
hosts file and for running the app installer.

## Where the binaries come from

DevBox downloads the runtimes and services it manages at your request. All
downloads use HTTPS. Sources are the official upstream projects, with these
deliberate exceptions where upstream ships no Windows/macOS build:

| Component | Source | Note |
|---|---|---|
| Apache (Windows) | [Apache Lounge](https://www.apachelounge.com) | de-facto standard Windows builds |
| Redis (Windows) | [redis-windows/redis-windows](https://github.com/redis-windows/redis-windows) | community MSYS2 builds |
| PHP (macOS) | [shivammathur/php-builder-darwin](https://github.com/shivammathur/php-builder-darwin) | same builds used by GitHub Actions' `setup-php` |
| Python (macOS) | [python-build-standalone](https://github.com/indygreg/python-build-standalone) | widely used standalone builds |

Verification status: Go downloads are strictly verified against upstream
SHA-256 sums; Node.js, PHP (Windows) and Rust are verified whenever the
upstream checksum files are reachable. Extending strict checksum verification
to the remaining downloads (services, tools, the app's own updater) is tracked
and being rolled out — if this matters for your environment, verify the
published `SHA256SUMS-windows.txt` on each release manually.

DevBox sends no telemetry. The only network requests are the downloads you
trigger and version/update checks. See the
[privacy policy](https://devbox.harungecit.dev/privacy.html).
