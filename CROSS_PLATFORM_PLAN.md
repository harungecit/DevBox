# DevBox Cross-Platform & Headless Plan

Bu doküman, DevBox'ın macOS/Linux desteği ve headless (desktop ortamı olmayan) kullanım senaryoları için mimari planı içerir.

## Mevcut Durum

Uygulama %100 Windows-only. Windows'a bağımlılık katmanları:

| Katman | Nerede | Ne yapıyor |
|--------|--------|------------|
| Win32 DLL (shell32, user32) | `internal/pathenv/`, `internal/project/hosts.go` | Registry PATH, UAC elevation, WM_SETTINGCHANGE |
| Windows Registry | `internal/pathenv/`, `autostart_windows.go` | HKCU PATH, autostart |
| syscall Windows API | `service/process.go`, `project/devserver.go`, `project/scaffold.go`, `runtime/php_cgi.go`, `tunnel/cloudflared.go` | CREATE_NEW_PROCESS_GROUP, HideWindow, OpenProcess, GetExitCodeProcess(==259) |
| PowerShell / cmd.exe | `project/hosts.go`, `project/ssl.go`, `tunnel/cloudflared.go`, `app.go`, `pathenv/` | Hosts yazma, mkcert indirme, dosya açma |
| Windows-only binary URL'leri | Tüm runtime ve service manager'lar | go windows/amd64, node win-x64, php windows.php.net, nginx.zip vb. |
| .exe / .cmd / .bat referansları | Her yerde | go.exe, php.exe, npx.cmd, composer.bat vb. |
| NSIS installer | `build/windows/installer/` | Windows installer |
| Wails Windows options | `main.go` | WebView2 tema ayarları |

Cross-platform soyutlama yok. `_linux.go` veya `_darwin.go` dosyası yok. Tek platform-aware dosya: `autostart_windows.go` (Go filename convention ile sadece Windows'ta derlenir).

## Hedef Mimari

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Wails Desktop│  │  Web UI      │  │  CLI         │
│ (Win/Mac/Lin)│  │  (headless)  │  │  (headless)  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │   Wails IPC     │   HTTP/WS       │  direkt
       └────────┬────────┴────────┬────────┘
                │                 │
        ┌───────▼─────────────────▼───────┐
        │   app.go (aynı metodlar)        │
        ├─────────────────────────────────┤
        │   internal/runtime/   (saf Go)  │
        │   internal/service/   (saf Go)  │
        │   internal/project/   (saf Go)  │
        │   internal/config/    (saf Go)  │
        │   internal/i18n/      (saf Go)  │
        ├─────────────────────────────────┤
        │   internal/platform/  (YENİ)    │
        │     platform.go     (interface) │
        │     windows.go                  │
        │     darwin.go                   │
        │     linux.go                    │
        └─────────────────────────────────┘
```

## Aşamalar

### Aşama 1 — Platform Interface Çıkar

Yeni `internal/platform/` paketi. Tüm Windows-specific operasyonları tek bir interface arkasına al.

```go
// internal/platform/platform.go
type Platform interface {
    // PATH yönetimi
    AddToPath(entry string) error
    RemoveFromPath(entry string) error
    GetUserPATH() (string, error)
    SetUserPATH(path string) error
    SwitchRuntimePath(runtimeName, oldDir, newDir string) error
    GetSystemPATH() (string, error)
    BroadcastPathChange() // Windows: WM_SETTINGCHANGE, diğerleri: no-op

    // Hosts dosyası (elevation dahil)
    ReadHostsFile() ([]byte, error)
    WriteHostsFile(content []byte) error

    // Process yönetimi
    SetProcessGroup(cmd *exec.Cmd)        // Windows: CREATE_NEW_PROCESS_GROUP, POSIX: Setpgid
    SetHideWindow(cmd *exec.Cmd)          // Windows: HideWindow=true, POSIX: no-op
    IsProcessRunning(pid int) bool        // Windows: OpenProcess+GetExitCodeProcess, POSIX: kill -0

    // Autostart
    SetAutoStart(enabled bool) error
    IsAutoStartEnabled() (bool, error)

    // Sistem entegrasyonu
    OpenInDefaultApp(path string) error   // Windows: cmd /c start, macOS: open, Linux: xdg-open
    BinaryName(base string) string        // Windows: base+".exe", diğerleri: base
    ScriptExt() string                    // Windows: ".bat", diğerleri: ".sh"

    // İndirme URL'leri (OS + arch aware)
    RuntimeDownloadInfo(name, version string) DownloadInfo
    ServiceDownloadInfo(name, version string) DownloadInfo
}

type DownloadInfo struct {
    URL           string
    ArchiveFormat string // "zip", "tar.gz"
    BinarySubPath string // arşiv içindeki binary'nin yolu
}
```

**İş:** Mevcut `internal/pathenv/`, `autostart_windows.go`, `internal/project/hosts.go` process.go'daki Windows kodları → `internal/platform/windows.go`'ya taşınır (move, rewrite değil). Diğer paketler `platform.Current().XYZ()` çağırır.

**Mevcut kodu kırar mı:** Hayır. Aynı davranış, sadece dolaylı çağrı.

### Aşama 2 — Mevcut Paketlerde platform.Current() Kullan

Tüm hardcoded Windows çağrılarını platform interface'ine yönlendir:

- `internal/runtime/*.go` → `platform.Current().BinaryName("go")` yerine hardcoded `"go.exe"`
- `internal/runtime/*.go` → `platform.Current().RuntimeDownloadInfo(name, ver)` yerine hardcoded Windows URL
- `internal/service/*.go` → `platform.Current().ServiceDownloadInfo(name, ver)` yerine hardcoded Windows URL
- `internal/service/process.go` → `platform.Current().SetProcessGroup(cmd)` yerine `cmd.SysProcAttr = &syscall.SysProcAttr{...}`
- `internal/project/scaffold.go` → `platform.Current().BinaryName("npx")` yerine `"npx.cmd"`
- `app.go` → `platform.Current().OpenInDefaultApp(path)` yerine `exec.Command("cmd", "/c", "start", ...)`

**Mevcut kodu kırar mı:** Hayır. Davranış aynı, `platform.Current()` Windows'ta Windows implementasyonunu döner.

### Aşama 3 — HTTP/WebSocket API Layer (Headless Web UI)

Yeni entry point: `cmd/devbox-server/main.go`

```go
// Aynı Svelte frontend'i HTTP server olarak sun
// Wails IPC yerine REST API + WebSocket

http.Handle("/", http.FileServer(frontendFS))   // embed edilen Svelte build
http.Handle("/api/", apiRouter)                   // app.go metodları REST endpoint
http.Handle("/ws", websocketHandler)              // EventsEmit → WebSocket push
```

Frontend'te ince bir bridge adapter:
```typescript
// frontend/src/lib/bridge/index.ts
// Wails varsa → window.go.main.App.Method()
// Web modda   → fetch('/api/Method', { body: args })
// Otomatik detect: typeof window.__wails__ !== 'undefined'
```

Kullanım:
```bash
devbox serve --port 9090
# Tarayıcıdan: http://sunucu-ip:9090
```

**Mevcut kodu kırar mı:** Hayır. Yeni entry point, mevcut Wails modu aynen çalışır.

### Aşama 4 — CLI Aracı

Yeni entry point: `cmd/devbox/main.go`

```bash
devbox runtime list go
devbox runtime install go 1.24.0
devbox runtime use go 1.24.0
devbox service start nginx
devbox service stop nginx
devbox project add ./mysite --domain mysite.test
devbox project list
devbox serve                # Web UI başlat (Aşama 3)
```

Aynı internal paketleri direkt çağırır. Progress → terminal spinner + yüzde çubuğu.

**Mevcut kodu kırar mı:** Hayır. Yeni entry point.

### Aşama 5 — Linux Platform Implementasyonu

`internal/platform/linux.go`:

| Operasyon | Linux Karşılığı |
|-----------|-----------------|
| PATH | `~/.bashrc` veya `~/.profile`'a `export PATH="..."` satırı ekle/çıkar |
| Hosts yazma (desktop) | `pkexec tee /etc/hosts` (Polkit GUI — GNOME/KDE'de çalışır) |
| Hosts yazma (headless) | `sudo tee /etc/hosts` (CLI'dan çağrılır, TTY gerektirir) |
| Process group | `syscall.SysProcAttr{Setpgid: true}` + `syscall.Kill(-pgid, signal)` |
| HideWindow | no-op (Linux'ta konsol gizleme yok) |
| IsProcessRunning | `syscall.Kill(pid, 0)` — hata yoksa process yaşıyor |
| Autostart | `~/.config/autostart/devbox.desktop` dosyası oluştur/sil |
| OpenInDefaultApp | `xdg-open path` |
| Binary isimleri | `.exe` yok: `"go"`, `"php"`, `"npx"` |
| İndirme URL'leri | `linux/amd64` binary'leri (go, node, vb.) |
| Installer | `.deb`, `.rpm`, AppImage, veya Flatpak |

### Aşama 6 — macOS Platform Implementasyonu

`internal/platform/darwin.go`:

| Operasyon | macOS Karşılığı |
|-----------|-----------------|
| PATH | `~/.zshrc`'ye `export PATH="..."` satırı ekle/çıkar |
| Hosts yazma | `osascript -e 'do shell script "tee /etc/hosts" with administrator privileges'` |
| Process group | `syscall.SysProcAttr{Setpgid: true}` + `syscall.Kill(-pgid, signal)` |
| HideWindow | no-op |
| IsProcessRunning | `syscall.Kill(pid, 0)` |
| Autostart | `~/Library/LaunchAgents/com.devbox.app.plist` |
| OpenInDefaultApp | `open path` |
| Binary isimleri | `.exe` yok |
| İndirme URL'leri | `darwin/arm64` (Apple Silicon) + `darwin/amd64` (Intel) |
| Installer | `.dmg` veya Homebrew tap |

### Aşama 7 — Wails Desktop macOS/Linux Build

`main.go`'da platform-aware Wails options:

```go
// +build darwin
wails.Run(&options.App{
    Mac: &mac.Options{...},
})

// +build linux
wails.Run(&options.App{
    Linux: &linux.Options{...},
})
```

## Headless Linux Senaryosu (Desktop Ortamı Yok)

Örnek: saf Debian sunucu, X11/Wayland yok.

**Web UI modu (önerilen):**
```bash
# Sunucuda
devbox serve --port 9090 --host 0.0.0.0

# Kendi bilgisayarından
# http://sunucu-ip:9090 → aynı Svelte arayüz
```

**CLI modu:**
```bash
devbox runtime install node 22.0.0
devbox service install nginx --port 80
devbox service start nginx
devbox project add /var/www/mysite --domain mysite.test
```

**Daemon modu (opsiyonel gelecekte):**
```bash
devbox daemon start   # arka planda çalışır, servisleri yönetir
devbox daemon stop
```

## Dosya Yapısı (Hedef)

```
DevBox/
├── main.go                          # Wails desktop entry (mevcut)
├── app.go                           # IPC metodları (mevcut)
├── cmd/
│   ├── devbox/main.go               # CLI entry point (Aşama 4)
│   └── devbox-server/main.go        # Web UI server entry (Aşama 3)
├── internal/
│   ├── platform/                    # YENİ (Aşama 1)
│   │   ├── platform.go              # Interface + Current() init
│   │   ├── windows.go               # Mevcut Windows kodları buraya taşınır
│   │   ├── darwin.go                # Aşama 6
│   │   └── linux.go                 # Aşama 5
│   ├── api/                         # YENİ (Aşama 3)
│   │   ├── router.go                # HTTP API router
│   │   └── websocket.go             # Event bridge
│   ├── runtime/                     # Mevcut (Aşama 2'de platform.Current() kullanacak)
│   ├── service/                     # Mevcut (Aşama 2'de platform.Current() kullanacak)
│   ├── project/                     # Mevcut (Aşama 2'de platform.Current() kullanacak)
│   ├── config/                      # Mevcut (değişmez)
│   ├── pathenv/                     # Aşama 1'de platform/'a taşınır
│   └── i18n/                        # Mevcut (değişmez)
├── frontend/
│   └── src/lib/bridge/              # YENİ (Aşama 3) — Wails↔HTTP adapter
└── build/
    ├── windows/                     # Mevcut
    ├── darwin/                      # Aşama 6 — macOS bundle
    └── linux/                       # Aşama 5 — .deb, AppImage vb.
```

## Kurallar

1. **Her aşama bağımsız.** Herhangi bir aşamada durulabilir, mevcut Windows uygulaması her zaman çalışır.
2. **Mevcut kod kırılmaz.** Refactor = move + wrap, rewrite değil.
3. **Öncelik sırası:** Aşama 1 → 2 → 3 → 4 → 5 → 6 → 7. Aşama 1-2 temel, diğerleri isteğe bağlı.
4. **Test:** Her aşamada `go build ./...` + `wails dev` ile Windows'ta çalıştığı doğrulanır.
