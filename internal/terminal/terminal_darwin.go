//go:build darwin

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func appInstalled(name string) string {
	for _, p := range []string{"/Applications/" + name + ".app", filepath.Join(os.Getenv("HOME"), "Applications", name+".app")} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// detect lists macOS terminals in preference order. zsh is the default shell.
func detect() []Terminal {
	iterm := appInstalled("iTerm")
	term := appInstalled("Terminal")
	if term == "" {
		term = "/System/Applications/Utilities/Terminal.app"
	}
	return []Terminal{
		{ID: "iterm", Name: "iTerm2", Shell: "zsh", Path: iterm, Installed: iterm != ""},
		{ID: "terminal", Name: "Terminal", Shell: "zsh", Path: term, Installed: true},
	}
}

// shellCommand builds the command the terminal runs: a zsh whose ZDOTDIR
// sources the user's ~/.zshrc first and then DevBox's init script.
func shellCommand(script string) (string, error) {
	zdot := filepath.Join(filepath.Dir(script), strings.TrimSuffix(filepath.Base(script), ".sh")+"-zdot")
	if err := os.MkdirAll(zdot, 0755); err != nil {
		return "", err
	}
	rc := fmt.Sprintf("[ -f ~/.zshrc ] && source ~/.zshrc\nsource %s\n", shQuote(script))
	if err := os.WriteFile(filepath.Join(zdot, ".zshrc"), []byte(rc), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("ZDOTDIR=%s exec /bin/zsh -i", shQuote(zdot)), nil
}

func osascript(lines ...string) error {
	args := []string{}
	for _, l := range lines {
		args = append(args, "-e", l)
	}
	return exec.Command("osascript", args...).Run()
}

func asQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func launch(t Terminal, s Session, script string) error {
	cmdline, err := shellCommand(script)
	if err != nil {
		return err
	}
	switch t.ID {
	case "iterm":
		return osascript(
			`tell application "iTerm"`,
			`activate`,
			`set w to (create window with default profile)`,
			`tell current session of w to write text `+asQuote(cmdline),
			`end tell`)
	default:
		return osascript(
			`tell application "Terminal"`,
			`activate`,
			`do script `+asQuote(cmdline),
			`end tell`)
	}
}
