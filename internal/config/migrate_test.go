package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFold(t *testing.T) {
	got := replaceFold(`dir C:/users/user/.devbox/services and C:/USERS/User/.devbox/x`, `C:/Users/User/.devbox`, `C:/DevBox`)
	want := `dir C:/DevBox/services and C:/DevBox/x`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRewriteEmbeddedPaths(t *testing.T) {
	root := t.TempDir()
	old := `C:\Users\User\.devbox`
	svc := filepath.Join(root, "services", "mysql")
	os.MkdirAll(filepath.Join(svc, "data"), 0755)
	os.MkdirAll(filepath.Join(root, "runtimes", "php", "8.4.16"), 0755)

	os.WriteFile(filepath.Join(svc, "my.ini"), []byte("basedir=C:/Users/User/.devbox/services/mysql\n"), 0644)
	os.WriteFile(filepath.Join(svc, "devbox-service.json"), []byte(`{"path":"C:\\Users\\User\\.devbox\\services\\mysql"}`), 0644)
	os.WriteFile(filepath.Join(svc, "data", "ibdata1"), []byte("C:/Users/User/.devbox must stay"), 0644)
	os.WriteFile(filepath.Join(root, "runtimes", "php", "8.4.16", "php.ini"), []byte(`extension_dir = "C:/Users/User/.devbox/runtimes/php/8.4.16/ext"`), 0644)

	n := rewriteEmbeddedPaths(root, old, root)
	if n != 3 {
		t.Fatalf("rewrote %d files, want 3", n)
	}
	check := func(rel, want string) {
		data, _ := os.ReadFile(filepath.Join(root, rel))
		if string(data) != want {
			t.Errorf("%s: got %q want %q", rel, string(data), want)
		}
	}
	fwd := filepath.ToSlash(root)
	check(filepath.Join("services", "mysql", "my.ini"), "basedir="+fwd+"/services/mysql\n")
	check(filepath.Join("services", "mysql", "devbox-service.json"), `{"path":"`+escapeJSON(root)+`\\services\\mysql"}`)
	check(filepath.Join("services", "mysql", "data", "ibdata1"), "C:/Users/User/.devbox must stay")
	check(filepath.Join("runtimes", "php", "8.4.16", "php.ini"), `extension_dir = "`+fwd+`/runtimes/php/8.4.16/ext"`)
}

func escapeJSON(p string) string {
	out := ""
	for _, r := range p {
		if r == '\\' {
			out += `\\`
		} else {
			out += string(r)
		}
	}
	return out
}
