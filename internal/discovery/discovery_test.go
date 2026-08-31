package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Version regexes must extract the right version from real-world tool output.
func TestVersionRegexes(t *testing.T) {
	cases := []struct {
		tool   string
		output string
		want   string
	}{
		{"node", "v22.11.0\n", "22.11.0"},
		{"go", "go version go1.24.1 windows/amd64\n", "1.24.1"},
		{"php", "PHP 8.3.14 (cli) (built: Nov 20 2024 17:35:28) (NTS Visual C++ 2022 x64)\n", "8.3.14"},
		{"python", "Python 3.12.4\n", "3.12.4"},
		{"rust", "rustc 1.83.0 (90b35a623 2024-11-26)\n", "1.83.0"},
	}
	for _, c := range cases {
		var found bool
		for _, spec := range runtimeSpecs {
			if spec.name != c.tool {
				continue
			}
			found = true
			if got := firstMatch(spec.verRe, c.output); got != c.want {
				t.Errorf("%s: got %q, want %q", c.tool, got, c.want)
			}
		}
		if !found {
			t.Errorf("no runtime spec for %s", c.tool)
		}
	}

	svcCases := []struct {
		tool   string
		output string
		want   string
	}{
		{"nginx", "nginx version: nginx/1.27.4\n", "1.27.4"},
		{"apache", "Server version: Apache/2.4.62 (Win64)\nServer built:   ...\n", "2.4.62"},
		{"caddy", "v2.9.1 h1:OEYiZ7DbCzAWVb6TNEkjRcSCRGHVoZsJinoDR/n9oaY=\n", "2.9.1"},
		{"postgres", "pg_ctl (PostgreSQL) 17.2\n", "17.2"},
		{"mysql", `C:\Program Files\MySQL\MySQL Server 8.0\bin\mysqld.exe  Ver 8.0.40 for Win64 on x86_64 (MySQL Community Server - GPL)` + "\n", "8.0.40"},
		{"mariadb", "mariadbd.exe  Ver 11.4.4-MariaDB for Win64 on x86_64 (mariadb.org binary distribution)\n", "11.4.4"},
		{"mongodb", "db version v8.0.4\nBuild Info: ...\n", "8.0.4"},
		{"redis", "Redis server v=7.4.2 sha=00000000:0 malloc=jemalloc-5.3.0 bits=64 build=abc\n", "7.4.2"},
		{"mailpit", "v1.21.5\n", "1.21.5"},
	}
	for _, c := range svcCases {
		var found bool
		for _, spec := range serviceSpecs {
			if spec.name != c.tool {
				continue
			}
			found = true
			if got := firstMatch(spec.verRe, c.output); got != c.want {
				t.Errorf("%s: got %q, want %q", c.tool, got, c.want)
			}
		}
		if !found {
			t.Errorf("no service spec for %s", c.tool)
		}
	}
}

// The MySQL spec must reject MariaDB's mysqld and vice versa.
func TestMySQLMariaDBClassification(t *testing.T) {
	mariadbOut := "mysqld.exe  Ver 10.11.10-MariaDB for Win64 on x86_64\n"
	mysqlOut := "mysqld.exe  Ver 8.0.40 for Win64 on x86_64 (MySQL Community Server - GPL)\n"

	var mysqlSpec, mariadbSpec serviceSpec
	for _, s := range serviceSpecs {
		if s.name == "mysql" {
			mysqlSpec = s
		}
		if s.name == "mariadb" {
			mariadbSpec = s
		}
	}

	// mysql spec rejects MariaDB output
	if mysqlSpec.reject == "" || !strings.Contains(mariadbOut, mysqlSpec.reject) {
		t.Error("mysql spec should reject MariaDB output")
	}
	// mariadb spec requires "MariaDB", which MySQL output lacks
	if mariadbSpec.require == "" || strings.Contains(mysqlOut, mariadbSpec.require) {
		t.Error("mariadb spec should not match MySQL output")
	}
}

func TestIsExcludedRoot(t *testing.T) {
	excluded := []string{
		`C:\Users\U\AppData\Local\Microsoft\WindowsApps`,
		`C:\Users\U\.cargo\bin`,
		`C:\ProgramData\chocolatey\bin`,
		`C:\Users\U\scoop\shims`,
		`C:\proj\node_modules\.bin\x`,
	}
	for _, p := range excluded {
		if !isExcludedRoot(p) {
			t.Errorf("expected excluded: %s", p)
		}
	}
	allowed := []string{
		`C:\Program Files\nodejs`,
		`C:\Program Files\PostgreSQL\17`,
		`/opt/homebrew/opt/node`,
	}
	for _, p := range allowed {
		if isExcludedRoot(p) {
			t.Errorf("expected allowed: %s", p)
		}
	}
}

func TestPlausibleServiceRoot(t *testing.T) {
	// nginx: requires conf/nginx.conf
	dir := t.TempDir()
	if plausibleServiceRoot("nginx", dir) {
		t.Error("bare dir should not be a plausible nginx root")
	}
	os.MkdirAll(filepath.Join(dir, "conf"), 0755)
	os.WriteFile(filepath.Join(dir, "conf", "nginx.conf"), []byte("events {}"), 0644)
	if !plausibleServiceRoot("nginx", dir) {
		t.Error("dir with conf/nginx.conf should be a plausible nginx root")
	}

	// redis: a shared bin directory is not plausible
	shared := t.TempDir()
	if plausibleServiceRoot("redis", shared) {
		t.Error("shared dir should not be a plausible redis root")
	}
	os.WriteFile(filepath.Join(shared, "redis.conf"), []byte(""), 0644)
	if !plausibleServiceRoot("redis", shared) {
		t.Error("dir with redis.conf should be a plausible redis root")
	}

	// others: no extra requirement
	if !plausibleServiceRoot("postgres", t.TempDir()) {
		t.Error("postgres root should have no extra plausibility requirement")
	}
}

func TestDedupeByVersion(t *testing.T) {
	in := []Found{
		{Kind: "runtime", Name: "node", Version: "22.11.0", Path: `C:\Program Files\nodejs`},
		{Kind: "runtime", Name: "node", Version: "22.11.0", Path: `C:\somewhere\else`},
		{Kind: "runtime", Name: "node", Version: "20.18.0", Path: `C:\nvm\v20.18.0`},
	}
	out := dedupeByVersion(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].Path != `C:\Program Files\nodejs` {
		t.Errorf("first hit should win, got %s", out[0].Path)
	}
}
