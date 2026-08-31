package service

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// TestImportExternalService verifies the in-place service import with a fake
// external Redis: binaries are hardlinked (not moved), DevBox writes its own
// config and data dir, and uninstalling never touches the external files.
func TestImportExternalService(t *testing.T) {
	if goruntime.GOOS != "windows" && goruntime.GOOS != "darwin" {
		t.Skip("platform-dependent (needs platform implementation)")
	}

	cfg := config.Get()
	oldDataDir := cfg.DataDir
	cfg.DataDir = t.TempDir()
	defer func() { cfg.DataDir = oldDataDir }()

	Register(NewRedisManager())

	// Fake external Redis install (directory name carries "redis" so the
	// plausibility check passes).
	src := filepath.Join(t.TempDir(), "redis-7.4.2")
	os.MkdirAll(src, 0755)
	serverExe := filepath.Join(src, platform.BinaryName("redis-server"))
	os.WriteFile(serverExe, []byte("fake"), 0755)
	os.WriteFile(filepath.Join(src, platform.BinaryName("redis-cli")), []byte("fake"), 0755)
	os.WriteFile(filepath.Join(src, "msys-2.0.dll"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(src, "redis.conf"), []byte("# external"), 0644)

	if err := ImportExternal("redis", src, "7.4.2", nil); err != nil {
		t.Fatalf("ImportExternal: %v", err)
	}

	base := serviceBaseDir("redis")
	for _, want := range []string{
		platform.BinaryName("redis-server"),
		platform.BinaryName("redis-cli"),
		"msys-2.0.dll",
		"redis.conf", // DevBox-written config, not the external one
	} {
		if _, err := os.Stat(filepath.Join(base, want)); err != nil {
			t.Errorf("expected %s in service dir: %v", want, err)
		}
	}
	// The DevBox config must be DevBox's own, not the external file.
	if data, _ := os.ReadFile(filepath.Join(base, "redis.conf")); string(data) == "# external" {
		t.Error("external redis.conf was reused; DevBox must write its own")
	}
	svcCfg := LoadServiceConfig("redis")
	// The port starts at the default and may step past ports busy on the
	// machine running the test.
	if svcCfg.Version != "7.4.2" || svcCfg.Port < 6379 {
		t.Errorf("unexpected service config: %+v", svcCfg)
	}

	// Uninstall removes the DevBox dir; the external install must survive.
	if err := NewRedisManager().Uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(serverExe); err != nil {
		t.Fatalf("external installation was damaged by uninstall: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(src, "redis.conf")); err != nil || string(data) != "# external" {
		t.Fatal("external redis.conf was modified")
	}
}

func TestCopyTreeExcluding(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	mustWrite := func(rel, content string) {
		p := filepath.Join(src, rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("bin/server.exe", "binary")
	mustWrite("conf/app.conf", "conf")
	mustWrite("data/user.db", "precious")
	mustWrite("logs/old.log", "log")
	mustWrite("Data/nested/x.db", "case-insensitive exclude")

	err := copyTreeExcluding(src, dst, map[string]bool{"data": true, "logs": true})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"bin/server.exe", "conf/app.conf"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("expected %s to be copied: %v", want, err)
		}
	}
	for _, unwanted := range []string{"data", "logs", "Data"} {
		if _, err := os.Stat(filepath.Join(dst, unwanted)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be excluded", unwanted)
		}
	}
}

func TestCopySubdirs(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	os.MkdirAll(filepath.Join(src, "bin"), 0755)
	os.WriteFile(filepath.Join(src, "bin", "pg_ctl"), []byte("x"), 0755)
	os.MkdirAll(filepath.Join(src, "share"), 0755)
	os.WriteFile(filepath.Join(src, "share", "messages"), []byte("y"), 0644)
	// no lib/ — optional, must not fail

	if err := copySubdirs(src, dst, []string{"bin"}, []string{"lib", "share"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "bin", "pg_ctl")); err != nil {
		t.Error("required bin/ not copied")
	}
	if _, err := os.Stat(filepath.Join(dst, "share", "messages")); err != nil {
		t.Error("optional share/ not copied")
	}

	// missing required dir must fail
	if err := copySubdirs(t.TempDir(), dst, []string{"bin"}, nil); err == nil {
		t.Error("expected error when required subdir is missing")
	}
}
