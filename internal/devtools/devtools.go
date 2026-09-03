// Package devtools is the registry of optional developer tools DevBox can
// install next to its runtimes: package managers / helpers per runtime
// (uv, pipx, Poetry, air, golangci-lint, cargo-watch…) and browser-based
// management UIs for services (Redis Commander, mongo-express). One generic
// install / uninstall / start / stop flow drives all of them; the Tools page
// renders whatever this catalog lists.
package devtools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/pathenv"
	"DevBox/internal/platform"
	"DevBox/internal/runtime"
	"DevBox/internal/service"
)

// Kind tells the generic installer how a tool is obtained.
type Kind string

const (
	KindPip    Kind = "pip"    // python -m pip install <pkg>   (into the active Python)
	KindGo     Kind = "go"     // go install <pkg>@latest       (GOBIN = <data>/tools/gobin)
	KindCargo  Kind = "cargo"  // cargo install <pkg>           (root  = <data>/tools/cargo)
	KindBinary Kind = "binary" // GitHub release asset          (<data>/tools/<id>/)
	KindNpm    Kind = "npm"    // npm install -g --prefix <data>/tools/<id> <pkg>  (web UIs)
)

// Tool is one catalog entry.
type Tool struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"`   // python | go | rust | service-ui
	Runtime  string `json:"runtime"` // runtime required to install/run
	Kind     Kind   `json:"kind"`
	Pkg      string `json:"pkg"`      // pip/go/cargo/npm package
	Bin      string `json:"bin"`      // executable name (without extension)
	Desc     string `json:"desc"`     // i18n key
	Homepage string `json:"homepage"` // docs link
	// verArgs overrides the version probe args (default ["--version"]); some
	// CLIs print their version through a subcommand instead (e.g. `wails version`).
	verArgs []string
	// Web tools
	Port        int      `json:"port"`        // fixed loopback port of the UI
	ForServices []string `json:"forServices"` // services this UI manages (first installed one is used)

	// binary kind
	ghOwner, ghRepo string
	asset           func() string // asset name for this OS/arch
	// web kind: how to launch the UI against a service port
	run func(binPath string, uiPort, servicePort int) *exec.Cmd
}

// Status is what the frontend renders.
type Status struct {
	Tool
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Running   bool   `json:"running"`
	URL       string `json:"url"`
	// Available: the runtime the tool needs is installed/active.
	Available bool `json:"available"`
	// ServiceName: the installed service this UI would manage ("" = none).
	ServiceName string `json:"serviceName"`
}

// Progress is streamed while installing.
type Progress struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

var Catalog = []Tool{
	// ---- Python ----
	{ID: "uv", Name: "uv", Group: "python", Runtime: "python", Kind: KindBinary, Bin: "uv",
		Desc: "tools.uvDesc", Homepage: "https://docs.astral.sh/uv/",
		ghOwner: "astral-sh", ghRepo: "uv", asset: func() string {
			switch goruntime.GOOS {
			case "darwin":
				if goruntime.GOARCH == "arm64" {
					return "uv-aarch64-apple-darwin.tar.gz"
				}
				return "uv-x86_64-apple-darwin.tar.gz"
			}
			return "uv-x86_64-pc-windows-msvc.zip"
		}},
	{ID: "pipx", Name: "pipx", Group: "python", Runtime: "python", Kind: KindPip, Pkg: "pipx", Bin: "pipx",
		Desc: "tools.pipxDesc", Homepage: "https://pipx.pypa.io/"},
	{ID: "poetry", Name: "Poetry", Group: "python", Runtime: "python", Kind: KindPip, Pkg: "poetry", Bin: "poetry",
		Desc: "tools.poetryDesc", Homepage: "https://python-poetry.org/"},

	// ---- Go ----
	{ID: "air", Name: "Air", Group: "go", Runtime: "go", Kind: KindGo, Pkg: "github.com/air-verse/air", Bin: "air",
		Desc: "tools.airDesc", Homepage: "https://github.com/air-verse/air"},
	{ID: "golangci-lint", Name: "golangci-lint", Group: "go", Runtime: "go", Kind: KindGo,
		Pkg: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", Bin: "golangci-lint",
		Desc: "tools.golangciDesc", Homepage: "https://golangci-lint.run/"},
	{ID: "gopls", Name: "gopls", Group: "go", Runtime: "go", Kind: KindGo, Pkg: "golang.org/x/tools/gopls", Bin: "gopls",
		Desc: "tools.goplsDesc", Homepage: "https://pkg.go.dev/golang.org/x/tools/gopls", verArgs: []string{"version"}},
	{ID: "wails", Name: "Wails", Group: "go", Runtime: "go", Kind: KindGo, Pkg: "github.com/wailsapp/wails/v2/cmd/wails", Bin: "wails",
		Desc: "tools.wailsDesc", Homepage: "https://wails.io/", verArgs: []string{"version"}},

	// ---- Rust ----
	{ID: "cargo-watch", Name: "cargo-watch", Group: "rust", Runtime: "rust", Kind: KindCargo, Pkg: "cargo-watch", Bin: "cargo-watch",
		Desc: "tools.cargoWatchDesc", Homepage: "https://github.com/watchexec/cargo-watch"},
	{ID: "cargo-edit", Name: "cargo-edit", Group: "rust", Runtime: "rust", Kind: KindCargo, Pkg: "cargo-edit", Bin: "cargo-add",
		Desc: "tools.cargoEditDesc", Homepage: "https://github.com/killercup/cargo-edit"},
	{ID: "cargo-audit", Name: "cargo-audit", Group: "rust", Runtime: "rust", Kind: KindCargo, Pkg: "cargo-audit", Bin: "cargo-audit",
		Desc: "tools.cargoAuditDesc", Homepage: "https://github.com/rustsec/rustsec"},

	// ---- Service management UIs (Node-based) ----
	{ID: "redis-commander", Name: "Redis Commander", Group: "service-ui", Runtime: "node", Kind: KindNpm,
		Pkg: "redis-commander", Bin: "redis-commander", Port: 8506, ForServices: []string{"redis", "valkey"},
		Desc: "tools.redisCommanderDesc", Homepage: "https://github.com/joeferner/redis-commander",
		run: func(bin string, uiPort, svcPort int) *exec.Cmd {
			args := []string{"--redis-host", "127.0.0.1", "--redis-port", strconv.Itoa(svcPort),
				"--address", "127.0.0.1", "--port", strconv.Itoa(uiPort), "--no-save"}
			for _, svc := range []string{"redis", "valkey"} {
				if mgr, ok := service.Registry[svc]; ok && mgr.IsInstalled() {
					if _, pw := service.Credentials(svc); pw != "" {
						args = append(args, "--redis-password", pw)
					}
					break
				}
			}
			return exec.Command(bin, args...)
		}},
	{ID: "mongo-express", Name: "mongo-express", Group: "service-ui", Runtime: "node", Kind: KindNpm,
		Pkg: "mongo-express", Bin: "mongo-express", Port: 8507, ForServices: []string{"mongodb"},
		Desc: "tools.mongoExpressDesc", Homepage: "https://github.com/mongo-express/mongo-express",
		run: func(bin string, uiPort, svcPort int) *exec.Cmd {
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(),
				"ME_CONFIG_MONGODB_URL=mongodb://127.0.0.1:"+strconv.Itoa(svcPort)+"/",
				"ME_CONFIG_BASICAUTH=false",
				"ME_CONFIG_SITE_BASEURL=/",
				"VCAP_APP_HOST=127.0.0.1",
				"VCAP_APP_PORT="+strconv.Itoa(uiPort),
				"PORT="+strconv.Itoa(uiPort),
			)
			return cmd
		}},
}

func lookup(id string) *Tool {
	for i := range Catalog {
		if Catalog[i].ID == id {
			return &Catalog[i]
		}
	}
	return nil
}

// ---- locations ----

func toolsDir() string           { return filepath.Join(config.GetDataDir(), "tools") }
func goBinDir() string           { return filepath.Join(toolsDir(), "gobin") }
func cargoRoot() string          { return filepath.Join(toolsDir(), "cargo") }
func cargoBinDir() string        { return filepath.Join(cargoRoot(), "bin") }
func npmPrefix(id string) string { return filepath.Join(toolsDir(), id) }
func binaryDir(id string) string { return filepath.Join(toolsDir(), id) }
func pidPath(id string) string   { return filepath.Join(toolsDir(), id+".pid") }
func logPath(id string) string   { return filepath.Join(toolsDir(), id+".log") }

// runtimeBinDir returns the bin dir of the globally active version of a runtime.
func runtimeBinDir(name string) string {
	mgr, ok := runtime.Registry[name]
	if !ok {
		return ""
	}
	ver, err := mgr.GetGlobal()
	if err != nil || ver == "" {
		return ""
	}
	return mgr.BinaryPath(ver)
}

// pythonScriptsDir is where pip puts console scripts for the active Python.
func pythonScriptsDir() string {
	bin := runtimeBinDir("python")
	if bin == "" {
		return ""
	}
	if goruntime.GOOS == "windows" {
		return filepath.Join(bin, "Scripts")
	}
	return bin
}

// BinPath returns where the tool's executable lives once installed.
func BinPath(t *Tool) string {
	switch t.Kind {
	case KindPip:
		return filepath.Join(pythonScriptsDir(), platform.BinaryName(t.Bin))
	case KindGo:
		return filepath.Join(goBinDir(), platform.BinaryName(t.Bin))
	case KindCargo:
		return filepath.Join(cargoBinDir(), platform.BinaryName(t.Bin))
	case KindBinary:
		return filepath.Join(binaryDir(t.ID), platform.BinaryName(t.Bin))
	case KindNpm:
		if goruntime.GOOS == "windows" {
			return filepath.Join(npmPrefix(t.ID), t.Bin+".cmd")
		}
		return filepath.Join(npmPrefix(t.ID), "bin", t.Bin)
	}
	return ""
}

func isInstalled(t *Tool) bool {
	p := BinPath(t)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// ---- versions (cached; a version probe spawns the tool) ----

var (
	versionMu    sync.Mutex
	versionCache = map[string]string{}
)

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

func version(t *Tool) string {
	versionMu.Lock()
	if v, ok := versionCache[t.ID]; ok {
		versionMu.Unlock()
		return v
	}
	versionMu.Unlock()

	v := ""
	switch t.Kind {
	case KindNpm:
		// package.json inside the prefix — no process needed.
		if b, err := os.ReadFile(filepath.Join(npmPrefix(t.ID), "node_modules", t.Pkg, "package.json")); err == nil {
			var pj struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(b, &pj) == nil {
				v = pj.Version
			}
		}
	default:
		args := t.verArgs
		if len(args) == 0 {
			args = []string{"--version"}
		}
		cmd := exec.Command(BinPath(t), args...)
		platform.SetProcessAttrs(cmd, false, true)
		if out, err := cmd.CombinedOutput(); err == nil {
			v = versionRe.FindString(string(out))
		}
	}
	versionMu.Lock()
	versionCache[t.ID] = v
	versionMu.Unlock()
	return v
}

func forgetVersion(id string) {
	versionMu.Lock()
	delete(versionCache, id)
	versionMu.Unlock()
}

// serviceFor returns the first installed service a web tool manages.
func serviceFor(t *Tool) (string, int) {
	for _, name := range t.ForServices {
		if mgr, ok := service.Registry[name]; ok && mgr.IsInstalled() {
			return name, mgr.Port()
		}
	}
	return "", 0
}

// List reports every catalog entry with its current state.
func List() []Status {
	out := make([]Status, 0, len(Catalog))
	for i := range Catalog {
		t := &Catalog[i]
		s := Status{Tool: *t}
		s.Available = runtimeBinDir(t.Runtime) != ""
		s.Installed = isInstalled(t)
		if s.Installed {
			s.Version = version(t)
		}
		if t.Kind == KindNpm {
			s.ServiceName, _ = serviceFor(t)
			s.Running = isRunning(t.ID)
			if s.Running {
				s.URL = fmt.Sprintf("http://127.0.0.1:%d/", t.Port)
			}
		}
		out = append(out, s)
	}
	return out
}

// ---- install / uninstall ----

func envWith(binDir string, extra ...string) []string {
	env := os.Environ()
	if binDir != "" {
		env = append(env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return append(env, extra...)
}

// Install obtains a tool. It blocks; callers run it on a goroutine and
// forward progress lines to the UI.
func Install(id string, progress chan<- Progress) error {
	t := lookup(id)
	if t == nil {
		return fmt.Errorf("unknown tool: %s", id)
	}
	report := func(msg string) {
		if progress != nil {
			progress <- Progress{ID: id, Message: msg}
		}
	}
	binDir := runtimeBinDir(t.Runtime)
	if t.Runtime != "" && binDir == "" {
		return fmt.Errorf("%s needs an active %s version", t.Name, t.Runtime)
	}
	if t.Kind == KindNpm {
		// A management UI without its service is useless — refuse instead of
		// letting the user find out at "Open".
		if svc, _ := serviceFor(t); svc == "" {
			return fmt.Errorf("%s manages %s — install that service first", t.Name, strings.Join(t.ForServices, " / "))
		}
	}
	os.MkdirAll(toolsDir(), 0755)
	defer forgetVersion(id)

	var cmd *exec.Cmd
	var pathDir string
	switch t.Kind {
	case KindPip:
		cmd = exec.Command(filepath.Join(binDir, platform.BinaryName("python")), "-m", "pip", "install", "--upgrade", t.Pkg)
		cmd.Env = envWith(binDir)
		pathDir = pythonScriptsDir()
	case KindGo:
		os.MkdirAll(goBinDir(), 0755)
		cmd = exec.Command(filepath.Join(binDir, platform.BinaryName("go")), "install", t.Pkg+"@latest")
		cmd.Env = envWith(binDir, "GOBIN="+goBinDir(), "GOFLAGS=-mod=mod")
		pathDir = goBinDir()
	case KindCargo:
		os.MkdirAll(cargoRoot(), 0755)
		cmd = exec.Command(filepath.Join(binDir, platform.BinaryName("cargo")), "install", "--locked", "--root", cargoRoot(), t.Pkg)
		cmd.Env = envWith(binDir)
		pathDir = cargoBinDir()
	case KindNpm:
		prefix := npmPrefix(t.ID)
		os.MkdirAll(prefix, 0755)
		cmd = exec.Command(filepath.Join(binDir, platform.ScriptName("npm")), "install", "-g", "--prefix", prefix, t.Pkg+"@latest")
		cmd.Env = envWith(binDir)
	case KindBinary:
		return installBinary(t, report)
	}

	report(fmt.Sprintf("Installing %s…", t.Name))
	cmd.Dir = toolsDir()
	platform.SetProcessAttrs(cmd, true, true)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			report(l)
		}
	}
	if err != nil {
		return fmt.Errorf("%s install failed: %w", t.Name, err)
	}
	if !isInstalled(t) {
		return fmt.Errorf("%s install finished but %s was not found", t.Name, BinPath(t))
	}
	if pathDir != "" {
		pathenv.AddToPath(pathDir)
	}
	return nil
}

func installBinary(t *Tool, report func(string)) error {
	assetName := t.asset()
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", t.ghOwner, t.ghRepo, assetName)
	if rels, err := runtime.FetchGitHubReleasesPublic(t.ghOwner, t.ghRepo); err == nil && len(rels) > 0 {
		for _, a := range rels[0].Assets {
			if a.Name == assetName {
				downloadURL = a.BrowserDownloadURL
				break
			}
		}
	}
	report("Downloading " + assetName)
	tmp := filepath.Join(config.GetDataDir(), "tmp", assetName)
	os.MkdirAll(filepath.Dir(tmp), 0755)
	if err := runtime.DownloadFile(downloadURL, tmp, 0, nil); err != nil {
		return err
	}
	defer os.Remove(tmp)
	extract := tmp + "-extract"
	os.RemoveAll(extract)
	defer os.RemoveAll(extract)
	var err error
	if strings.HasSuffix(assetName, ".zip") {
		err = runtime.ExtractZip(tmp, extract, nil)
	} else {
		err = runtime.ExtractTarGz(tmp, extract, nil)
	}
	if err != nil {
		return err
	}
	// The binary may sit at the root or one directory down.
	want := platform.BinaryName(t.Bin)
	var found string
	filepath.Walk(extract, func(p string, info os.FileInfo, _ error) error {
		if found == "" && info != nil && !info.IsDir() && info.Name() == want {
			found = p
		}
		return nil
	})
	if found == "" {
		return fmt.Errorf("%s not found in %s", want, assetName)
	}
	dir := binaryDir(t.ID)
	os.MkdirAll(dir, 0755)
	data, err := os.ReadFile(found)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, want), data, 0755); err != nil {
		return err
	}
	pathenv.AddToPath(dir)
	report(t.Name + " installed")
	return nil
}

// Uninstall removes a tool (stopping its server first for web tools).
func Uninstall(id string) error {
	t := lookup(id)
	if t == nil {
		return fmt.Errorf("unknown tool: %s", id)
	}
	defer forgetVersion(id)
	switch t.Kind {
	case KindPip:
		binDir := runtimeBinDir(t.Runtime)
		if binDir == "" {
			return fmt.Errorf("no active python")
		}
		cmd := exec.Command(filepath.Join(binDir, platform.BinaryName("python")), "-m", "pip", "uninstall", "-y", t.Pkg)
		platform.SetProcessAttrs(cmd, false, true)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pip uninstall: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case KindGo:
		return os.Remove(BinPath(t))
	case KindCargo:
		binDir := runtimeBinDir(t.Runtime)
		if binDir != "" {
			cmd := exec.Command(filepath.Join(binDir, platform.BinaryName("cargo")), "uninstall", "--root", cargoRoot(), t.Pkg)
			platform.SetProcessAttrs(cmd, false, true)
			cmd.Run()
		}
		os.Remove(BinPath(t))
		return nil
	case KindBinary:
		return os.RemoveAll(binaryDir(t.ID))
	case KindNpm:
		Stop(id)
		return os.RemoveAll(npmPrefix(t.ID))
	}
	return nil
}

// ---- web UIs ----

func isRunning(id string) bool {
	data, err := os.ReadFile(pidPath(id))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return platform.IsProcessRunning(pid)
}

// Start launches a web tool against the service it manages. Idempotent.
func Start(id string) (string, error) {
	t := lookup(id)
	if t == nil || t.Kind != KindNpm {
		return "", fmt.Errorf("%s is not a web tool", id)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", t.Port)
	if isRunning(id) {
		return url, nil
	}
	if !isInstalled(t) {
		return "", fmt.Errorf("%s is not installed", t.Name)
	}
	svc, svcPort := serviceFor(t)
	if svc == "" {
		return "", fmt.Errorf("%s manages %s — install that service first", t.Name, strings.Join(t.ForServices, "/"))
	}
	if mgr := service.Registry[svc]; mgr.Status() != service.StatusRunning {
		if err := mgr.Start(); err != nil {
			return "", fmt.Errorf("could not start %s: %w", svc, err)
		}
	}
	logF, err := os.OpenFile(logPath(id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	// Run the package's JS entry with node directly instead of the npm .cmd
	// shim: the shim is a cmd.exe wrapper, so killing its PID on Stop would
	// orphan the real node process and the UI would keep serving.
	nodeBin := runtimeBinDir("node")
	entry := npmEntry(t)
	if entry == "" {
		return "", fmt.Errorf("%s: cannot find the package entry script", t.Name)
	}
	cmd := t.run(BinPath(t), t.Port, svcPort)
	cmd.Path = filepath.Join(nodeBin, platform.BinaryName("node"))
	cmd.Args = append([]string{cmd.Path, entry}, cmd.Args[1:]...)
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "PATH="+nodeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Dir = npmPrefix(t.ID)
	cmd.Stdout = logF
	cmd.Stderr = logF
	platform.SetProcessAttrs(cmd, true, true)
	if err := cmd.Start(); err != nil {
		logF.Close()
		return "", fmt.Errorf("failed to start %s: %w", t.Name, err)
	}
	os.WriteFile(pidPath(id), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	go func() {
		cmd.Wait()
		logF.Close()
	}()
	time.Sleep(700 * time.Millisecond)
	if !isRunning(id) {
		os.Remove(pidPath(id))
		return "", fmt.Errorf("%s exited immediately — port %d may be in use (see %s)", t.Name, t.Port, logPath(id))
	}
	return url, nil
}

// npmEntry resolves the "bin" script of an npm web tool to an absolute path.
func npmEntry(t *Tool) string {
	pkgDir := filepath.Join(npmPrefix(t.ID), "node_modules", t.Pkg)
	b, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return ""
	}
	var pj struct {
		Bin json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(b, &pj) != nil || len(pj.Bin) == 0 {
		return ""
	}
	var single string
	if json.Unmarshal(pj.Bin, &single) == nil && single != "" {
		return filepath.Join(pkgDir, filepath.FromSlash(single))
	}
	var many map[string]string
	if json.Unmarshal(pj.Bin, &many) == nil {
		if p, ok := many[t.Bin]; ok {
			return filepath.Join(pkgDir, filepath.FromSlash(p))
		}
		for _, p := range many {
			return filepath.Join(pkgDir, filepath.FromSlash(p))
		}
	}
	return ""
}

// Stop kills a web tool's process. No-op when not running.
func Stop(id string) error {
	data, err := os.ReadFile(pidPath(id))
	if err != nil {
		return nil
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
		platform.KillProcessTree(pid)
	}
	os.Remove(pidPath(id))
	return nil
}

// StopAll stops every running web tool (app shutdown).
func StopAll() {
	for _, t := range Catalog {
		if t.Kind == KindNpm {
			Stop(t.ID)
		}
	}
}

// LogPath exposes a tool's log for diagnostics.
func LogPath(id string) string { return logPath(id) }
