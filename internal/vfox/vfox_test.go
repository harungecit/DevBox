package vfox

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"DevBox/internal/config"
)

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "plugins", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func useTempDataDir(t *testing.T) string {
	t.Helper()
	cfg := config.Get()
	old := cfg.DataDir
	dir := t.TempDir()
	cfg.DataDir = dir
	t.Cleanup(func() { cfg.DataDir = old })
	return dir
}

func loadFixture(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(fixtureDir(t, "fixture"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestLoadMetadata(t *testing.T) {
	p := loadFixture(t)
	if p.Meta.Name != "fixture" || p.Meta.Version != "0.1.0" || p.Meta.License != "Apache 2.0" {
		t.Fatalf("metadata not decoded: %+v", p.Meta)
	}
	if len(p.Meta.Notes) != 1 || p.Meta.Notes[0] != "test note" {
		t.Fatalf("notes: %+v", p.Meta.Notes)
	}
	if len(p.Meta.LegacyFilenames) != 1 || p.Meta.LegacyFilenames[0] != ".fixture-version" {
		t.Fatalf("legacyFilenames: %+v", p.Meta.LegacyFilenames)
	}
	if !p.HasHook("PostInstall") || !p.HasHook("PreUninstall") || p.HasHook("PreUse") {
		t.Fatal("HasHook mismatch")
	}
}

func TestAvailableAndModules(t *testing.T) {
	p := loadFixture(t)
	items, err := p.Available(nil)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(items) != 3 || items[0].Version != "2.0.0" || items[2].Note != "LTS" {
		t.Fatalf("unexpected list: %+v", items)
	}
	if len(items[2].Addition) != 1 || items[2].Addition[0].Name != "helper" {
		t.Fatalf("addition not decoded: %+v", items[2].Addition)
	}
}

func TestPreInstallResolvesAliasAndEmptyResult(t *testing.T) {
	t.Setenv("DEVBOX_FIXTURE_ARCHIVE", "C:/nope/pkg.zip")
	p := loadFixture(t)
	r, err := p.PreInstall("latest")
	if err != nil {
		t.Fatalf("PreInstall: %v", err)
	}
	if r.Version != "2.0.0" || r.Path != "C:/nope/pkg.zip" || r.Note != "from fixture" {
		t.Fatalf("unexpected result: %+v", r.PreInstallPackageItem)
	}
	if len(r.Addition) != 1 || r.Addition[0].Name != "helper" || r.Addition[0].Version != "9" {
		t.Fatalf("addition: %+v", r.Addition)
	}
	if _, err := p.PreInstall("missing"); err == nil || !strings.Contains(err.Error(), "no installable version") {
		t.Fatalf("expected no-version error, got %v", err)
	}
}

func TestPostInstallShellAndPopen(t *testing.T) {
	p := loadFixture(t)
	root := t.TempDir()
	main := filepath.Join(root, "fixture-1.0")
	os.MkdirAll(main, 0755)
	var log bytes.Buffer
	var lines []string
	p.SetExecContext(root, nil, &log, func(l string) { lines = append(lines, l) })
	err := p.PostInstall(&PostInstallHookCtx{
		RootPath: root,
		SdkInfo:  map[string]*InstalledPackageItem{"fixture": {Name: "fixture", Version: "1.0", Path: main}},
	})
	if err != nil {
		t.Fatalf("PostInstall: %v (log: %s)", err, log.String())
	}
	if _, err := os.Stat(filepath.Join(main, "post.txt")); err != nil {
		t.Fatalf("os.execute did not run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "post-ok"))
	if err != nil || !strings.Contains(string(data), "popen-works") {
		t.Fatalf("io.popen shim failed: %v %q", err, data)
	}
	if !strings.Contains(log.String(), "$ echo") {
		t.Fatalf("command not logged: %s", log.String())
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "popen-works") {
			found = true
		}
	}
	if !found {
		t.Fatalf("output lines not reported: %v", lines)
	}
}

func TestEnvKeys(t *testing.T) {
	p := loadFixture(t)
	keys, err := p.EnvKeys(&EnvKeysHookCtx{
		Main: &InstalledPackageItem{Name: "fixture", Version: "2.0.0", Path: "/sdk/fixture-2.0.0"},
		Path: "/sdk/fixture-2.0.0",
		SdkInfo: map[string]*InstalledPackageItem{
			"fixture": {Name: "fixture", Version: "2.0.0", Path: "/sdk/fixture-2.0.0"},
			"helper":  {Name: "helper", Version: "9", Path: "/sdk/add-helper-9"},
		},
	})
	if err != nil {
		t.Fatalf("EnvKeys: %v", err)
	}
	var paths []string
	vars := map[string]string{}
	for _, k := range keys {
		if k.Key == "PATH" {
			paths = append(paths, k.Value)
		} else {
			vars[k.Key] = k.Value
		}
	}
	if len(paths) != 2 || paths[1] != "/sdk/add-helper-9" {
		t.Fatalf("paths: %v", paths)
	}
	if vars["FIXTURE_HOME"] != "/sdk/fixture-2.0.0" || vars["FIXTURE_VERSION"] != "2.0.0" {
		t.Fatalf("vars: %v", vars)
	}
}

func TestParseLegacyFileColonCall(t *testing.T) {
	p := loadFixture(t)
	dir := t.TempDir()
	file := filepath.Join(dir, ".fixture-version")
	os.WriteFile(file, []byte("installed\n"), 0644)
	r, err := p.ParseLegacyFile(&ParseLegacyFileHookCtx{
		Filepath:             file,
		Filename:             ".fixture-version",
		GetInstalledVersions: func() []string { return []string{"3.3.3", "1.1.1"} },
	})
	if err != nil {
		t.Fatalf("ParseLegacyFile: %v", err)
	}
	if r.Version != "3.3.3" {
		t.Fatalf("expected latest installed, got %q", r.Version)
	}
	os.WriteFile(file, []byte(" 20 "), 0644)
	r, err = p.ParseLegacyFile(&ParseLegacyFileHookCtx{Filepath: file, Filename: ".fixture-version"})
	if err != nil || r.Version != "20" {
		t.Fatalf("specified version: %v %+v", err, r)
	}
}

func TestPreUninstall(t *testing.T) {
	p := loadFixture(t)
	root := t.TempDir()
	main := filepath.Join(root, "fixture-1.0")
	os.MkdirAll(main, 0755)
	if err := p.PreUninstall(&PreUninstallHookCtx{Main: &InstalledPackageItem{Name: "fixture", Version: "1.0", Path: main}}); err != nil {
		t.Fatalf("PreUninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "pre-uninstall-ran")); err != nil {
		t.Fatal("PreUninstall hook did not run")
	}
}

func TestTimeoutRebuildsVM(t *testing.T) {
	old := timeoutAvailable
	timeoutAvailable = 300 * time.Millisecond
	defer func() { timeoutAvailable = old }()

	p := loadFixture(t)
	start := time.Now()
	_, err := p.Available([]string{"hang"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not honoured: %v", time.Since(start))
	}
	if p.vm != nil {
		t.Fatal("state should be discarded after a timeout")
	}
	items, err := p.Available(nil)
	if err != nil || len(items) != 3 {
		t.Fatalf("plugin unusable after rebuild: %v", err)
	}
}

func TestOSExitIsBlocked(t *testing.T) {
	p := loadFixture(t)
	_, err := p.Available([]string{"exit"})
	if err == nil || !strings.Contains(err.Error(), "os.exit") {
		t.Fatalf("expected os.exit error, got %v", err)
	}
}

func TestLegacyMainLua(t *testing.T) {
	p, err := Load(fixtureDir(t, "legacy"))
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	defer p.Close()
	if p.Meta.Name != "legacy" {
		t.Fatalf("meta: %+v", p.Meta)
	}
	items, err := p.Available(nil)
	if err != nil || len(items) != 1 || items[0].Version != "1.0.0" {
		t.Fatalf("Available: %v %+v", err, items)
	}
	if err := p.PostInstall(&PostInstallHookCtx{}); err != nil {
		t.Fatalf("missing optional hook must be a no-op, got %v", err)
	}
	_, err = p.PreUse(&PreUseHookCtx{})
	if !errors.Is(err, ErrHookMissing) {
		t.Fatalf("expected ErrHookMissing, got %v", err)
	}
}

func TestLoadRejectsInvalidPlugins(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Fatal("empty dir must fail")
	}
	os.WriteFile(filepath.Join(dir, "metadata.lua"), []byte(`PLUGIN = { name = "bad name" }`), 0644)
	os.MkdirAll(filepath.Join(dir, "hooks"), 0755)
	for _, h := range []string{"available", "pre_install", "env_keys"} {
		os.WriteFile(filepath.Join(dir, "hooks", h+".lua"), []byte("function PLUGIN:X() end"), 0644)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "bad name") {
		t.Fatalf("expected bad name error, got %v", err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	w.Close()
	f.Close()
}

func TestStoreInstallListRemove(t *testing.T) {
	data := useTempDataDir(t)
	var msgs []string
	rec, err := Install("", fixtureDir(t, "fixture"), func(m string) { msgs = append(msgs, m) })
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if rec.Name != "fixture" || rec.Source != "local" || !rec.ThirdParty {
		t.Fatalf("record: %+v", rec)
	}
	if rec.Dir != filepath.Join(data, "plugins", "fixture") {
		t.Fatalf("dir: %s", rec.Dir)
	}
	if _, err := os.Stat(filepath.Join(rec.Dir, recordFile)); err != nil {
		t.Fatal("record file missing")
	}
	if _, err := os.Stat(filepath.Join(rec.Dir, "hooks", "available.lua")); err != nil {
		t.Fatal("plugin files not copied")
	}
	list, err := ListInstalled()
	if err != nil || len(list) != 1 || list[0].Name != "fixture" || list[0].Version != "0.1.0" {
		t.Fatalf("ListInstalled: %v %+v", err, list)
	}

	// Reinstall (update path) keeps the plugin usable and leaves no backup.
	if _, err := Install("", fixtureDir(t, "fixture"), nil); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(rec.Dir + "-bak"); !os.IsNotExist(err) {
		t.Fatal("backup dir left behind")
	}

	// A zip source with a root folder is accepted too.
	zipPath := filepath.Join(t.TempDir(), "plug.zip")
	writeZip(t, zipPath, map[string]string{
		"vfox-zipped-1.0/metadata.lua":          `PLUGIN = { name = "zipped", version = "1.0" }`,
		"vfox-zipped-1.0/hooks/available.lua":   `function PLUGIN:Available(ctx) return {} end`,
		"vfox-zipped-1.0/hooks/pre_install.lua": `function PLUGIN:PreInstall(ctx) return {} end`,
		"vfox-zipped-1.0/hooks/env_keys.lua":    `function PLUGIN:EnvKeys(ctx) return {} end`,
	})
	if _, err := Install("", zipPath, nil); err != nil {
		t.Fatalf("Install zip: %v", err)
	}
	if _, err := GetInstalled("zipped"); err != nil {
		t.Fatalf("zipped plugin not registered: %v", err)
	}

	// Remove refuses while versions exist, unless forced.
	os.MkdirAll(filepath.Join(data, "runtimes", "fixture", "2.0.0"), 0755)
	if err := Remove("fixture", false); err == nil {
		t.Fatal("Remove must refuse while versions are installed")
	}
	if err := Remove("fixture", true); err != nil {
		t.Fatalf("forced Remove: %v", err)
	}
	if _, err := GetInstalled("fixture"); err == nil {
		t.Fatal("plugin still listed after removal")
	}
}

func TestInstallRefusesBuiltinAliases(t *testing.T) {
	useTempDataDir(t)
	if _, err := Install("nodejs", "", nil); err == nil || !strings.Contains(err.Error(), "built into DevBox") {
		t.Fatalf("expected alias refusal, got %v", err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.lua"), []byte(`PLUGIN = { name = "golang" }
function PLUGIN:Available() return {} end
function PLUGIN:PreInstall() return {} end
function PLUGIN:EnvKeys() return {} end`), 0644)
	if _, err := Install("", dir, nil); err == nil || !strings.Contains(err.Error(), "built into DevBox") {
		t.Fatalf("expected alias refusal for local plugin, got %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10.0", "1.9.9", 1},
		{"v0.5.3", "0.5.3", 0},
		{"1.0.11", "0.3.0", 1},
		{"2.0.0-rc1", "2.0.0", -1},
		{"21", "21.0.1", -1},
	}
	for _, c := range cases {
		got := CompareVersions(c.a, c.b)
		if (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("CompareVersions(%q,%q)=%d want sign %d", c.a, c.b, got, c.want)
		}
	}
	if !IsPreRelease("2.1.0-rc1") || IsPreRelease("2.1.0") {
		t.Fatal("IsPreRelease")
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent("nodejs", "0.4.1")
	if !strings.HasPrefix(ua, "vfox/"+CompatVersion+" vfox-nodejs/0.4.1 DevBox/") {
		t.Fatalf("ua: %s", ua)
	}
}
