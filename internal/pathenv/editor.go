package pathenv

import (
	"fmt"
	"os"
	goruntime "runtime"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// Entry is one PATH directory as stored (unexpanded) plus what the UI needs.
type Entry struct {
	Path     string `json:"path"`     // raw registry value, e.g. %SystemRoot%\system32
	Expanded string `json:"expanded"` // with %VAR% resolved
	Exists   bool   `json:"exists"`
	Managed  bool   `json:"managed"` // inside the DevBox data dir
}

// Editor is the editable view of both PATH scopes.
type Editor struct {
	Supported bool    `json:"supported"` // system scope editable (Windows)
	System    []Entry `json:"system"`
	User      []Entry `json:"user"`
}

func toEntries(raw []string) []Entry {
	data := strings.ToLower(config.GetDataDir())
	out := make([]Entry, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		exp := expandWindowsVars(p)
		fi, err := os.Stat(exp)
		out = append(out, Entry{
			Path:     p,
			Expanded: exp,
			Exists:   err == nil && fi.IsDir(),
			Managed:  strings.HasPrefix(strings.ToLower(exp), data),
		})
	}
	return out
}

// GetEditor returns both scopes in their stored order.
func GetEditor() Editor {
	e := Editor{Supported: goruntime.GOOS == "windows", System: []Entry{}, User: []Entry{}}
	if user, err := platform.GetUserPATH(); err == nil {
		e.User = toEntries(user)
	}
	if e.Supported {
		if system, err := platform.GetMachinePATH(); err == nil {
			e.System = toEntries(system)
		}
	}
	return e
}

func validate(entries []string) ([]string, error) {
	var out []string
	for _, p := range entries {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, "\";\n\r") {
			return nil, fmt.Errorf("invalid PATH entry: %q", p)
		}
		out = append(out, p)
	}
	return out, nil
}

// SaveUser replaces the user PATH with the given order (backup kept).
func SaveUser(entries []string) error {
	clean, err := validate(entries)
	if err != nil {
		return err
	}
	if old, err := platform.GetUserPATH(); err == nil {
		backup("user", old)
	}
	return platform.SetUserPATH(clean)
}

// SaveSystem replaces the machine PATH (elevated write, UAC prompt). It
// refuses a PATH without the Windows system directory so a slip cannot leave
// the machine without cmd.exe, powershell.exe & co.
func SaveSystem(entries []string) error {
	if goruntime.GOOS != "windows" {
		return fmt.Errorf("the system PATH can only be edited on Windows")
	}
	clean, err := validate(entries)
	if err != nil {
		return err
	}
	hasSystem32 := false
	for _, p := range clean {
		if strings.Contains(strings.ToLower(expandWindowsVars(p)), `\system32`) {
			hasSystem32 = true
			break
		}
	}
	if !hasSystem32 {
		return fmt.Errorf("refusing to save: the system PATH must keep the Windows system32 directory")
	}
	if old, err := platform.GetMachinePATH(); err == nil {
		backup("system", old)
	}
	return platform.SetMachinePATHElevated(clean)
}

// Refresh tells every window/app that the environment changed and reloads
// DevBox's own PATH from the registry so terminals it opens see the new order.
func Refresh() error {
	platform.BroadcastPathChange()
	if goruntime.GOOS != "windows" {
		return nil
	}
	system, err := platform.GetSystemPATH()
	if err != nil {
		return err
	}
	user, err := platform.GetUserPATH()
	if err != nil {
		return err
	}
	var all []string
	for _, p := range append(system, user...) {
		if p = strings.TrimSpace(expandWindowsVars(p)); p != "" {
			all = append(all, p)
		}
	}
	return os.Setenv("PATH", strings.Join(all, string(os.PathListSeparator)))
}
