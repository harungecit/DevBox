package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/vfox"
	"DevBox/internal/vfox/archive"
)

// BuiltinRuntimes are the hand-written managers, in display order.
var BuiltinRuntimes = []string{"go", "node", "php", "python", "rust"}

var builtinDisplay = map[string]string{
	"go": "Go", "node": "Node.js", "php": "PHP", "python": "Python", "rust": "Rust",
}

// pluginDisplay gives friendly names to well-known registry plugins; anything
// else is capitalised.
var pluginDisplay = map[string]string{
	"java": "Java", "dotnet": ".NET", "ruby": "Ruby", "deno": "Deno", "bun": "Bun",
	"dart": "Dart", "flutter": "Flutter", "kotlin": "Kotlin", "zig": "Zig",
	"elixir": "Elixir", "erlang": "Erlang", "julia": "Julia", "gradle": "Gradle",
	"maven": "Maven", "terraform": "Terraform", "kubectl": "kubectl", "cmake": "CMake",
	"lua": "Lua", "crystal": "Crystal", "scala": "Scala", "groovy": "Groovy",
	"clang": "Clang", "vlang": "V", "typst": "Typst", "protobuf": "Protobuf",
	"vagrant": "Vagrant", "etcd": "etcd", "ninja": "Ninja", "make": "Make",
	"tomcat": "Tomcat", "chaosblade": "ChaosBlade", "grails": "Grails",
	"mongo": "MongoDB Shell", "mongod": "MongoDB Server", "gcc-arm-none-eabi": "GCC ARM",
	"helm": "Helm", "swift": "Swift", "nim": "Nim", "haskell": "Haskell", "perl": "Perl",
	"r": "R", "ocaml": "OCaml", "gleam": "Gleam", "odin": "Odin", "pnpm": "pnpm", "yarn": "Yarn",
}

// ToolPlugins are registry plugins that are tools rather than languages; the
// Projects page does not offer them as a project runtime.
var ToolPlugins = map[string]bool{
	"kubectl": true, "terraform": true, "maven": true, "gradle": true, "cmake": true,
	"ninja": true, "make": true, "protobuf": true, "vagrant": true, "etcd": true,
	"chaosblade": true, "tomcat": true, "helm": true, "mongo": true, "mongod": true,
	"gcc-arm-none-eabi": true, "typst": true, "grails": true,
}

const (
	installingMarker = ".devbox-installing"
	envFileName      = "devbox-env.json"
)

var (
	pluginsMu sync.RWMutex
	plugins   = map[string]*PluginManager{}
)

// IsBuiltinAlias reports whether a plugin name duplicates a built-in runtime.
func IsBuiltinAlias(name string) bool { return vfox.IsBuiltinAlias(name) }

// IsPluginRuntime reports whether name is a registered plugin-backed runtime.
func IsPluginRuntime(name string) bool {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	_, ok := plugins[name]
	return ok
}

// PluginInfo returns the plugin record behind a runtime.
func PluginInfo(name string) (vfox.InstalledPlugin, bool) {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if pm, ok := plugins[name]; ok {
		return pm.rec, true
	}
	return vfox.InstalledPlugin{}, false
}

// Names lists every registered runtime: built-ins in their fixed order, then
// plugin runtimes alphabetically.
func Names() []string {
	out := make([]string, 0, len(Registry))
	for _, n := range BuiltinRuntimes {
		if _, ok := Registry[n]; ok {
			out = append(out, n)
		}
	}
	var extra []string
	for n := range Registry {
		if _, ok := builtinDisplay[n]; !ok {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// DisplayName returns the human name of a runtime.
func DisplayName(name string) string {
	if d, ok := builtinDisplay[name]; ok {
		return d
	}
	if d, ok := pluginDisplay[name]; ok {
		return d
	}
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// RegisterPlugins loads every plugin under <data>/plugins and registers a
// manager for it. Half-finished installs from a crashed session are removed.
// Errors are returned per plugin; the others still register.
func RegisterPlugins() []error {
	installed, err := vfox.ListInstalled()
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, rec := range installed {
		if err := RegisterPlugin(rec.Name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rec.Name, err))
		}
	}
	return errs
}

// RegisterPlugin loads one installed plugin and registers its manager
// (replacing a previous registration, e.g. after an update).
func RegisterPlugin(name string) error {
	if IsBuiltinAlias(name) {
		return fmt.Errorf("%s duplicates a built-in runtime", name)
	}
	if _, ok := builtinDisplay[name]; ok {
		return fmt.Errorf("%s is a built-in runtime name", name)
	}
	rec, err := vfox.GetInstalled(name)
	if err != nil {
		return err
	}
	p, err := vfox.Load(rec.Dir)
	if err != nil {
		return err
	}
	pm := &PluginManager{name: name, p: p, rec: *rec, envCache: map[string]*versionEnv{}}
	pm.cleanupInstalling()
	pluginsMu.Lock()
	if old, ok := plugins[name]; ok {
		old.p.Close()
	}
	plugins[name] = pm
	pluginsMu.Unlock()
	Register(pm)
	return nil
}

// UnregisterPlugin forgets a plugin runtime (its files are left alone).
func UnregisterPlugin(name string) {
	pluginsMu.Lock()
	if pm, ok := plugins[name]; ok {
		pm.p.Close()
		delete(plugins, name)
	}
	pluginsMu.Unlock()
	if _, ok := builtinDisplay[name]; !ok {
		delete(Registry, name)
	}
}

// versionEnv is <version>/devbox-env.json: what EnvKeys returned right after
// installation, so PATH/env activation never has to run Lua again.
type versionEnv struct {
	Plugin        string                                `json:"plugin"`
	PluginVersion string                                `json:"pluginVersion"`
	Version       string                                `json:"version"`
	Main          *vfox.InstalledPackageItem            `json:"main"`
	SdkInfo       map[string]*vfox.InstalledPackageItem `json:"sdkInfo"`
	Paths         []string                              `json:"paths"`
	Vars          map[string]string                     `json:"vars"`
	InstalledAt   string                                `json:"installedAt"`
}

// PluginManager adapts a vfox plugin to the RuntimeManager interface.
type PluginManager struct {
	name string
	p    *vfox.Plugin
	rec  vfox.InstalledPlugin

	envMu    sync.Mutex
	envCache map[string]*versionEnv
}

// Plugin exposes the underlying vfox plugin (for legacy-file parsing etc.).
func (m *PluginManager) Plugin() *vfox.Plugin { return m.p }

// Record returns the installed-plugin record.
func (m *PluginManager) Record() vfox.InstalledPlugin { return m.rec }

func (m *PluginManager) Name() string { return m.name }

func (m *PluginManager) versionDir(version string) string {
	return filepath.Join(runtimeBaseDir(m.name), version)
}

func (m *PluginManager) cleanupInstalling() {
	entries, err := os.ReadDir(runtimeBaseDir(m.name))
	if err != nil {
		return
	}
	for _, e := range entries {
		dir := filepath.Join(runtimeBaseDir(m.name), e.Name())
		if _, err := os.Stat(filepath.Join(dir, installingMarker)); err == nil {
			os.RemoveAll(dir)
		}
	}
}

// stableFromNote derives Version.Stable the way the UI expects: an explicit
// "LTS"/"stable" note wins, a note naming a pre-release channel ("dev",
// "nightly", "rc"…) loses, anything else is decided by the version string.
func stableFromNote(items []*vfox.AvailableHookResultItem) func(*vfox.AvailableHookResultItem) bool {
	hasNotes := false
	for _, it := range items {
		if strings.TrimSpace(it.Note) != "" {
			hasNotes = true
			break
		}
	}
	unstableWords := []string{"dev", "nightly", "beta", "rc", "alpha", "preview", "master", "canary", "unstable", "snapshot", "ea"}
	return func(it *vfox.AvailableHookResultItem) bool {
		n := strings.ToLower(strings.TrimSpace(it.Note))
		if hasNotes && n != "" {
			if strings.Contains(n, "lts") || strings.Contains(n, "stable") {
				return true
			}
			for _, w := range unstableWords {
				if strings.Contains(n, w) {
					return false
				}
			}
			// Any other note ("latest", "release", vendor names…) says nothing
			// about stability — fall through to the version string.
		}
		return !vfox.IsPreRelease(it.Version)
	}
}

func (m *PluginManager) ListRemote() ([]Version, error) {
	items, err := m.p.Available(nil)
	if err != nil {
		return nil, err
	}
	stable := stableFromNote(items)
	global, _ := m.GetGlobal()
	out := make([]Version, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		v := strings.TrimSpace(it.Version)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, Version{Number: v, Stable: stable(it), Current: v == global})
	}
	return out, nil
}

func (m *PluginManager) ListInstalled() ([]Version, error) {
	versions, err := listInstalledVersions(m.name)
	if err != nil {
		return nil, err
	}
	global, _ := m.GetGlobal()
	var out []Version
	for _, v := range versions {
		if _, err := os.Stat(filepath.Join(m.versionDir(v), installingMarker)); err == nil {
			continue
		}
		out = append(out, Version{Number: v, Stable: true, Current: v == global})
	}
	sort.Slice(out, func(i, j int) bool { return vfox.CompareVersions(out[i].Number, out[j].Number) > 0 })
	if out == nil {
		out = []Version{}
	}
	return out, nil
}

// ResolveVersion asks the plugin which concrete version an input such as
// "latest" or "21" means, without downloading anything.
func (m *PluginManager) ResolveVersion(version string) (string, error) {
	r, err := m.p.PreInstall(version)
	if err != nil {
		return "", err
	}
	return r.Version, nil
}

func (m *PluginManager) logFile() string {
	dir := filepath.Join(config.GetDataDir(), "logs")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "plugin-"+m.name+".log")
}

// childEnv is the environment PostInstall commands run with: DevBox-managed
// bin dirs first (so a plugin can call node/python/git DevBox installed), then
// the SDK being installed, then the user's PATH.
func (m *PluginManager) childEnv(extraDirs ...string) []string {
	env := os.Environ()
	pathKey := "PATH"
	var kept []string
	userPath := ""
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "PATH") {
			pathKey = k
			userPath = v
			continue
		}
		kept = append(kept, kv)
	}
	dirs := append([]string{}, ManagedPathDirs()...)
	dirs = append(dirs, extraDirs...)
	if userPath != "" {
		dirs = append(dirs, userPath)
	}
	kept = append(kept, pathKey+"="+strings.Join(dirs, string(os.PathListSeparator)))
	if os.Getenv("VFOX_PYTHON_USE_UV_BUILD") == "" {
		kept = append(kept, "VFOX_PYTHON_USE_UV_BUILD=1") // prebuilt Pythons instead of source builds
	}
	return kept
}

func report(progress chan<- Progress, pct int, msg string) {
	if progress != nil {
		progress <- Progress{Percent: pct, Message: msg}
	}
}

func itemDirName(isMain bool, item *vfox.PreInstallPackageItem) string {
	name := item.Name
	if item.Version != "" {
		name += "-" + item.Version
	}
	if !isMain {
		name = "add-" + name
	}
	return name
}

// Install runs the vfox install pipeline for one version (see InstallResolved).
func (m *PluginManager) Install(version string, progress chan<- Progress) error {
	_, err := m.InstallResolved(version, progress)
	return err
}

// InstallResolved runs the vfox install pipeline for one version:
// PreInstall → download/extract main + additions → PostInstall → EnvKeys,
// materialised under <data>/runtimes/<plugin>/<version>/ with vfox's layout.
// It returns the concrete version the plugin resolved the input to
// ("latest" → "21.0.4"), which is also the directory name.
func (m *PluginManager) InstallResolved(version string, progress chan<- Progress) (resolved string, err error) {
	display := DisplayName(m.name)
	report(progress, 2, fmt.Sprintf("Resolving %s %s…", display, version))
	pre, err := m.p.PreInstall(version)
	if err != nil {
		return "", err
	}
	resolved = strings.TrimSpace(pre.Version)
	destDir := m.versionDir(resolved)
	if _, statErr := os.Stat(destDir); statErr == nil {
		return resolved, fmt.Errorf("%s %s is already installed", display, resolved)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return resolved, err
	}
	if err := os.WriteFile(filepath.Join(destDir, installingMarker), []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
		return resolved, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(destDir)
		}
	}()

	main := pre.PreInstallPackageItem
	if main.Name == "" {
		main.Name = m.name
	}
	items := []*vfox.PreInstallPackageItem{main}
	for _, a := range pre.Addition {
		if a != nil && a.Name != "" {
			items = append(items, a)
		}
	}
	sdkInfo := map[string]*vfox.InstalledPackageItem{}
	tmp, err := os.MkdirTemp(tmpDir(), "vfox-"+m.name+"-")
	if err != nil {
		return resolved, err
	}
	defer os.RemoveAll(tmp)

	n := len(items)
	for i, item := range items {
		isMain := i == 0
		lo := 5 + 60*i/n
		hi := 5 + 60*(i+1)/n
		itemDir := filepath.Join(destDir, itemDirName(isMain, item))
		if err = os.MkdirAll(itemDir, 0755); err != nil {
			return resolved, err
		}
		if err = m.materialise(item, itemDir, tmp, lo, hi, progress); err != nil {
			err = fmt.Errorf("%s: %w", item.Label(), err)
			return resolved, err
		}
		sdkInfo[item.Name] = &vfox.InstalledPackageItem{Name: item.Name, Version: item.Version, Path: itemDir, Note: item.Note}
	}
	mainInfo := sdkInfo[main.Name]

	if m.p.HasHook("PostInstall") {
		report(progress, 80, "Running post-install steps…")
		logF, _ := os.Create(m.logFile())
		if logF != nil {
			fmt.Fprintf(logF, "# %s %s post-install — %s\n", display, resolved, time.Now().Format(time.RFC3339))
			defer logF.Close()
		}
		m.p.SetExecContext(destDir, m.childEnv(mainInfo.Path, filepath.Join(mainInfo.Path, "bin")), logF, func(line string) {
			if len(line) > 160 {
				line = line[:157] + "…"
			}
			report(progress, 85, line)
		})
		m.p.SetSymlinkRoot(destDir)
		err = m.p.PostInstall(&vfox.PostInstallHookCtx{RootPath: destDir, SdkInfo: sdkInfo})
		m.p.SetExecContext(m.p.Dir, nil, nil, nil)
		if err != nil {
			err = fmt.Errorf("post-install failed (see %s): %w", m.logFile(), err)
			return resolved, err
		}
	}

	report(progress, 97, "Reading environment…")
	keys, err := m.p.EnvKeys(&vfox.EnvKeysHookCtx{Main: mainInfo, Path: mainInfo.Path, SdkInfo: sdkInfo})
	if err != nil {
		return resolved, err
	}
	env := &versionEnv{
		Plugin:        m.name,
		PluginVersion: m.rec.Version,
		Version:       resolved,
		Main:          mainInfo,
		SdkInfo:       sdkInfo,
		Vars:          map[string]string{},
		InstalledAt:   time.Now().UTC().Format(time.RFC3339),
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if k == nil {
			continue
		}
		if strings.EqualFold(k.Key, "PATH") {
			p := filepath.Clean(k.Value)
			if !seen[strings.ToLower(p)] {
				seen[strings.ToLower(p)] = true
				env.Paths = append(env.Paths, p)
			}
			continue
		}
		env.Vars[k.Key] = k.Value
	}
	if len(env.Paths) == 0 {
		env.Paths = []string{mainInfo.Path}
	}
	data, _ := json.MarshalIndent(env, "", "  ")
	if err = os.WriteFile(filepath.Join(destDir, envFileName), data, 0644); err != nil {
		return resolved, err
	}
	if err = os.Remove(filepath.Join(destDir, installingMarker)); err != nil {
		return resolved, err
	}
	m.envMu.Lock()
	m.envCache[resolved] = env
	m.envMu.Unlock()
	report(progress, 100, fmt.Sprintf("%s %s installed", display, resolved))
	return resolved, nil
}

// materialise puts one package's files into itemDir: download (or copy a
// local source), verify, extract when it is an archive.
func (m *PluginManager) materialise(item *vfox.PreInstallPackageItem, itemDir, tmp string, lo, hi int, progress chan<- Progress) error {
	src := strings.TrimSpace(item.Path)
	if src == "" {
		return nil // the plugin builds everything in PostInstall
	}
	var local string
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		report(progress, lo, fmt.Sprintf("Downloading %s…", item.Label()))
		lastPct := -1
		path, err := vfox.Download(context.Background(), src, tmp, item.Headers, vfox.UserAgent(m.name, m.rec.Version), func(done, total int64) {
			if total <= 0 {
				return
			}
			pct := lo + int(float64(hi-lo-3)*float64(done)/float64(total))
			if pct != lastPct {
				lastPct = pct
				report(progress, pct, fmt.Sprintf("Downloading %s… %.1f / %.1f MB", item.Label(), float64(done)/1048576, float64(total)/1048576))
			}
		})
		if err != nil {
			return err
		}
		local = path
		if cs := item.CheckSumItem.Pick(); cs != nil {
			report(progress, hi-3, "Verifying checksum…")
			if err := cs.Verify(local); err != nil {
				return err
			}
		}
	} else {
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("source not found: %s", src)
		}
		local = src
	}

	fi, err := os.Stat(local)
	if err != nil {
		return err
	}
	switch {
	case fi.IsDir():
		report(progress, hi-2, fmt.Sprintf("Copying %s…", item.Label()))
		return copyTree(local, itemDir, func(int, string) {})
	case archive.IsKnownArchive(local):
		report(progress, hi-2, fmt.Sprintf("Extracting %s…", item.Label()))
		return archive.Decompress(local, itemDir)
	default:
		// A bare file (installer, single binary): keep it under its own name.
		return copyFileMode(local, filepath.Join(itemDir, filepath.Base(local)))
	}
}

// sidecarEnvFile is where the env of an imported (linked) version lives:
// <runtimes>/<plugin>/<version>.devbox-env.json — outside the link target.
func (m *PluginManager) sidecarEnvFile(version string) string {
	return filepath.Join(runtimeBaseDir(m.name), version+"."+envFileName)
}

// WriteImportedEnv records the environment of an external installation that
// was linked/copied to rootDir (Import Center), using the plugin's EnvKeys hook
// with the installation root as the main package path.
func (m *PluginManager) WriteImportedEnv(version, rootDir string) error {
	main := &vfox.InstalledPackageItem{Name: m.name, Version: version, Path: rootDir}
	sdkInfo := map[string]*vfox.InstalledPackageItem{m.name: main}
	keys, err := m.p.EnvKeys(&vfox.EnvKeysHookCtx{Main: main, Path: rootDir, SdkInfo: sdkInfo})
	if err != nil {
		return err
	}
	env := &versionEnv{Plugin: m.name, PluginVersion: m.rec.Version, Version: version, Main: main, SdkInfo: sdkInfo, Vars: map[string]string{}, InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	seen := map[string]bool{}
	for _, k := range keys {
		if k == nil {
			continue
		}
		if strings.EqualFold(k.Key, "PATH") {
			p := filepath.Clean(k.Value)
			if !seen[strings.ToLower(p)] {
				seen[strings.ToLower(p)] = true
				env.Paths = append(env.Paths, p)
			}
			continue
		}
		env.Vars[k.Key] = k.Value
	}
	if len(env.Paths) == 0 {
		if bin := PluginBinaryInRoot(m.name, rootDir); bin != "" {
			env.Paths = []string{filepath.Dir(bin)}
		} else {
			env.Paths = []string{rootDir}
		}
	}
	data, _ := json.MarshalIndent(env, "", "  ")
	if err := os.WriteFile(m.sidecarEnvFile(version), data, 0644); err != nil {
		return err
	}
	m.envMu.Lock()
	m.envCache[version] = env
	m.envMu.Unlock()
	return nil
}

func (m *PluginManager) readEnv(version string) *versionEnv {
	m.envMu.Lock()
	defer m.envMu.Unlock()
	if e, ok := m.envCache[version]; ok {
		return e
	}
	data, err := os.ReadFile(filepath.Join(m.versionDir(version), envFileName))
	if err != nil {
		data, err = os.ReadFile(m.sidecarEnvFile(version))
	}
	if err != nil {
		return nil
	}
	var e versionEnv
	if json.Unmarshal(data, &e) != nil {
		return nil
	}
	m.envCache[version] = &e
	return &e
}

func (m *PluginManager) forgetEnv(version string) {
	m.envMu.Lock()
	delete(m.envCache, version)
	m.envMu.Unlock()
}

// EnvPaths implements EnvProvider.
func (m *PluginManager) EnvPaths(version string) []string {
	if e := m.readEnv(version); e != nil && len(e.Paths) > 0 {
		return append([]string{}, e.Paths...)
	}
	return []string{m.BinaryPath(version)}
}

// EnvVars implements EnvProvider.
func (m *PluginManager) EnvVars(version string) map[string]string {
	if e := m.readEnv(version); e != nil {
		out := make(map[string]string, len(e.Vars))
		for k, v := range e.Vars {
			out[k] = v
		}
		return out
	}
	return nil
}

// InstalledSdkInfo returns the package map recorded at install time.
func (m *PluginManager) InstalledSdkInfo(version string) (*vfox.InstalledPackageItem, map[string]*vfox.InstalledPackageItem) {
	if e := m.readEnv(version); e != nil {
		return e.Main, e.SdkInfo
	}
	main := &vfox.InstalledPackageItem{Name: m.name, Version: version, Path: m.mainDir(version)}
	return main, map[string]*vfox.InstalledPackageItem{m.name: main}
}

func (m *PluginManager) mainDir(version string) string {
	return filepath.Join(m.versionDir(version), m.name+"-"+version)
}

// BinaryPath returns the first PATH directory of the version (the main bin).
func (m *PluginManager) BinaryPath(version string) string {
	if version == "" {
		return ""
	}
	if e := m.readEnv(version); e != nil && len(e.Paths) > 0 {
		return e.Paths[0]
	}
	// No env record: an imported root without a sidecar, or the vfox layout.
	if bin := PluginBinaryInRoot(m.name, m.versionDir(version)); bin != "" {
		return filepath.Dir(bin)
	}
	if goruntime.GOOS == "windows" {
		return m.mainDir(version)
	}
	return filepath.Join(m.mainDir(version), "bin")
}

func (m *PluginManager) Uninstall(version string) error {
	if _, err := os.Lstat(m.versionDir(version)); os.IsNotExist(err) {
		return fmt.Errorf("version %s is not installed", version)
	}
	if m.p.HasHook("PreUninstall") {
		main, info := m.InstalledSdkInfo(version)
		m.p.SetExecContext(m.versionDir(version), m.childEnv(), nil, nil)
		err := m.p.PreUninstall(&vfox.PreUninstallHookCtx{Main: main, SdkInfo: info})
		m.p.SetExecContext(m.p.Dir, nil, nil, nil)
		if err != nil {
			return err
		}
	}
	global, _ := m.GetGlobal()
	if global == version {
		cfg := config.Get()
		delete(cfg.ActiveRuntimes, m.name)
		config.Save()
	}
	m.forgetEnv(version)
	os.Remove(m.sidecarEnvFile(version))
	return uninstallVersion(m.name, version)
}

func (m *PluginManager) SetGlobal(version string) error {
	if version == "" {
		cfg := config.Get()
		delete(cfg.ActiveRuntimes, m.name)
		return config.Save()
	}
	if _, err := os.Stat(m.versionDir(version)); err != nil {
		return fmt.Errorf("version %s is not installed", version)
	}
	return setGlobalVersion(m.name, version)
}

func (m *PluginManager) GetGlobal() (string, error) {
	return getGlobalVersion(m.name)
}

// ParseLegacyFile looks for the plugin's legacy version files (.nvmrc,
// .java-version…) in dir and asks the plugin which version they mean.
func (m *PluginManager) ParseLegacyFile(dir string) (string, bool) {
	if len(m.rec.LegacyFilenames) == 0 || !m.p.HasHook("ParseLegacyFile") {
		return "", false
	}
	for _, fn := range m.rec.LegacyFilenames {
		fp := filepath.Join(dir, fn)
		if fi, err := os.Stat(fp); err != nil || fi.IsDir() {
			continue
		}
		r, err := m.p.ParseLegacyFile(&vfox.ParseLegacyFileHookCtx{
			Filepath: fp,
			Filename: fn,
			GetInstalledVersions: func() []string {
				installed, _ := m.ListInstalled()
				out := make([]string, 0, len(installed))
				for _, v := range installed {
					out = append(out, v.Number)
				}
				return out
			},
			Strategy: "specified",
		})
		if err == nil && r != nil && strings.TrimSpace(r.Version) != "" {
			return strings.TrimSpace(r.Version), true
		}
	}
	return "", false
}

var errNotPlugin = errors.New("not a plugin runtime")

// PluginManagerFor returns the manager when name is a plugin runtime.
func PluginManagerFor(name string) (*PluginManager, error) {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if pm, ok := plugins[name]; ok {
		return pm, nil
	}
	return nil, errNotPlugin
}
