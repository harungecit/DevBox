package terminal

import (
	"os"
	"strings"
	"testing"
)

func TestRenderScripts(t *testing.T) {
	s := Session{Title: "myapp", Dir: `C:\DevBox\projects\myapp`, Path: []string{`C:\DevBox\runtimes\php\8.4.1`}, Env: map[string]string{"PORT": "3000"}, Cmd: "psql -d postgres"}

	ps := renderPowerShell("myapp", s)
	for _, want := range []string{"$env:PATH = 'C:\\DevBox\\runtimes\\php\\8.4.1'", "$env:PORT = '3000'", "Set-Location -LiteralPath 'C:\\DevBox\\projects\\myapp'", "psql -d postgres", "function global:prompt"} {
		if !strings.Contains(ps, want) {
			t.Errorf("powershell script missing %q:\n%s", want, ps)
		}
	}
	cmd := renderCmd("myapp", s)
	for _, want := range []string{`set "PATH=C:\DevBox\runtimes\php\8.4.1;%PATH%"`, `set "PORT=3000"`, `cd /d "C:\DevBox\projects\myapp"`, "psql -d postgres"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("cmd script missing %q:\n%s", want, cmd)
		}
	}
	sh := renderPosix("myapp", s)
	for _, want := range []string{"export PATH='/c/DevBox/runtimes/php/8.4.1':\"$PATH\"", "export PORT='3000'", "cd '/c/DevBox/projects/myapp'", "psql -d postgres", "PS1="} {
		if !strings.Contains(sh, want) {
			t.Errorf("posix script missing %q:\n%s", want, sh)
		}
	}
	if got := toPosixPath(`D:\x\y z`); got != "/d/x/y z" {
		t.Errorf("toPosixPath = %q", got)
	}
}

func TestDetectHasEntries(t *testing.T) {
	if len(detect()) == 0 {
		t.Fatal("no terminals in catalog")
	}
}

// TestSmokeOpen actually opens the first detected terminal; opt-in because it
// pops a window on the developer's desktop.
func TestSmokeOpen(t *testing.T) {
	if os.Getenv("DEVBOX_TERMINAL_SMOKE") == "" {
		t.Skip("set DEVBOX_TERMINAL_SMOKE=1 to open a real terminal")
	}
	id := os.Getenv("DEVBOX_TERMINAL_SMOKE")
	if id == "1" {
		id = ""
	}
	s := Session{Title: "smoke-test", Dir: os.TempDir(), Path: []string{`C:\DevBox\tools\bun`}, Env: map[string]string{"DEVBOX_SMOKE": "yes"}, Cmd: "echo DEVBOX_SMOKE=$env:DEVBOX_SMOKE"}
	var err error
	if id == "" {
		err = Open(s)
	} else {
		if strings.Contains(id, "bash") {
			s.Cmd = "echo DEVBOX_SMOKE=$DEVBOX_SMOKE"
		}
		err = OpenWith(id, s)
	}
	if err != nil {
		t.Fatal(err)
	}
}
