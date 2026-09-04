package pathenv

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

// CmdPathLimit is the longest PATH cmd.exe can expand. Beyond it cmd.exe (and
// therefore every .bat wrapper such as composer.bat) cannot resolve anything
// on PATH — the classic "'php' is not recognized" although php is installed.
const CmdPathLimit = 8191

// Health is the diagnosis of the machine + user PATH the Path page shows.
type Health struct {
	Supported bool `json:"supported"` // Windows only

	SystemEntries int `json:"systemEntries"`
	SystemUnique  int `json:"systemUnique"`
	UserEntries   int `json:"userEntries"`
	UserUnique    int `json:"userUnique"`

	SystemLength   int `json:"systemLength"`
	UserLength     int `json:"userLength"`
	CombinedLength int `json:"combinedLength"`
	Limit          int `json:"limit"`

	TooLong     bool `json:"tooLong"`     // combined length exceeds Limit
	LiteralPath bool `json:"literalPath"` // a literal %PATH% entry (self-reference)

	SystemDuplicates []string `json:"systemDuplicates"`
	SystemMissing    []string `json:"systemMissing"`
	UserDuplicates   []string `json:"userDuplicates"`
	UserMissing      []string `json:"userMissing"`

	// After counts what a cleanup would leave behind.
	SystemAfter int `json:"systemAfter"`
	UserAfter   int `json:"userAfter"`
	AfterLength int `json:"afterLength"`
	Issues      int `json:"issues"`

	// Shadowed lists DevBox-managed tools that another PATH entry wins over
	// (Windows resolves the machine PATH before the user PATH, so a Laragon
	// or XAMPP folder in the system PATH beats DevBox's user entries).
	Shadowed []Shadow `json:"shadowed"`
}

// Shadow is one tool whose first PATH hit is not the DevBox-managed copy.
type Shadow struct {
	Tool     string `json:"tool"`     // "composer", "php"…
	Expected string `json:"expected"` // DevBox dir that holds the tool
	Actual   string `json:"actual"`   // dir that currently wins
	System   bool   `json:"system"`   // Actual lives in the machine PATH
}

// shadowTools are the executables worth checking; plugin runtimes add theirs.
var shadowTools = []string{"php", "composer", "node", "npm", "npx", "go", "python", "pip", "cargo", "rustc",
	"java", "ruby", "deno", "bun", "dart", "kotlinc", "zig", "crystal", "dotnet", "mvn", "gradle", "kubectl", "terraform"}

func hasTool(dir, tool string, exists func(string) bool) bool {
	for _, ext := range []string{".exe", ".bat", ".cmd"} {
		if exists(filepath.Join(dir, tool+ext)) {
			return true
		}
	}
	return false
}

// findShadows walks system+user PATH in Windows order and reports every
// managed tool whose first hit is outside the managed dirs.
func findShadows(system, user, managed []string, exists func(string) bool) []Shadow {
	isManaged := map[string]bool{}
	for _, d := range managed {
		isManaged[strings.ToLower(strings.TrimRight(d, `\/`))] = true
	}
	type entry struct {
		dir    string
		system bool
	}
	var order []entry
	for _, d := range system {
		if d = strings.TrimSpace(d); d != "" && !strings.EqualFold(d, "%PATH%") {
			order = append(order, entry{expandWindowsVars(d), true})
		}
	}
	for _, d := range user {
		if d = strings.TrimSpace(d); d != "" {
			order = append(order, entry{expandWindowsVars(d), false})
		}
	}
	var out []Shadow
	for _, tool := range shadowTools {
		expected := ""
		for _, d := range managed {
			if hasTool(d, tool, exists) {
				expected = d
				break
			}
		}
		if expected == "" {
			continue
		}
		for _, e := range order {
			if !hasTool(e.dir, tool, exists) {
				continue
			}
			if !isManaged[strings.ToLower(strings.TrimRight(e.dir, `\/`))] {
				out = append(out, Shadow{Tool: tool, Expected: expected, Actual: e.dir, System: e.system})
			}
			break
		}
	}
	return out
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// CheckWith is Check plus the shadow analysis for the given managed dirs.
func CheckWith(managed []string) Health {
	h := Check()
	if !h.Supported {
		return h
	}
	system, _ := platform.GetMachinePATH()
	user, _ := platform.GetUserPATH()
	h.Shadowed = findShadows(system, user, managed, fileExists)
	if len(h.Shadowed) > 0 {
		h.Issues++
	}
	if h.Shadowed == nil {
		h.Shadowed = []Shadow{}
	}
	return h
}

// RemoveSystemEntry drops one directory from the machine PATH (elevated).
func RemoveSystemEntry(dir string) error {
	entries, err := platform.GetMachinePATH()
	if err != nil {
		return err
	}
	var kept []string
	norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(dir), `\/`))
	for _, e := range entries {
		if strings.ToLower(strings.TrimRight(strings.TrimSpace(e), `\/`)) == norm {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(entries) {
		return nil
	}
	backup("system", entries)
	return platform.SetMachinePATHElevated(kept)
}

func dirExists(p string) bool {
	if strings.Contains(p, "%") {
		p = expandWindowsVars(p)
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// expandWindowsVars expands %VAR% references using the current environment.
func expandWindowsVars(p string) string {
	out := p
	for i := 0; i < 10; i++ {
		start := strings.Index(out, "%")
		if start < 0 {
			break
		}
		end := strings.Index(out[start+1:], "%")
		if end < 0 {
			break
		}
		name := out[start+1 : start+1+end]
		val := os.Getenv(name)
		out = out[:start] + val + out[start+1+end+1:]
	}
	return out
}

// analyze is the pure part of Check (unit-tested): duplicates are
// case-insensitive after trimming a trailing separator.
func analyze(system, user []string, exists func(string) bool) Health {
	h := Health{Supported: true, Limit: CmdPathLimit}
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(p), `\/`))
	}
	scan := func(entries []string) (unique int, dups, missing []string, kept []string) {
		seen := map[string]bool{}
		for _, e := range entries {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if strings.EqualFold(e, "%PATH%") {
				h.LiteralPath = true
				continue
			}
			k := norm(e)
			if seen[k] {
				dups = append(dups, e)
				continue
			}
			seen[k] = true
			unique++
			if !exists(e) {
				missing = append(missing, e)
				continue
			}
			kept = append(kept, e)
		}
		return
	}
	var sysKept, userKept []string
	h.SystemUnique, h.SystemDuplicates, h.SystemMissing, sysKept = scan(system)
	h.UserUnique, h.UserDuplicates, h.UserMissing, userKept = scan(user)
	h.SystemEntries = len(system)
	h.UserEntries = len(user)
	h.SystemLength = len(strings.Join(system, ";"))
	h.UserLength = len(strings.Join(user, ";"))
	h.CombinedLength = h.SystemLength + 1 + h.UserLength
	h.TooLong = h.CombinedLength > h.Limit
	h.SystemAfter = len(sysKept)
	h.UserAfter = len(userKept)
	h.AfterLength = len(strings.Join(sysKept, ";")) + 1 + len(strings.Join(userKept, ";"))
	if h.TooLong {
		h.Issues++
	}
	if h.LiteralPath {
		h.Issues++
	}
	if len(h.SystemDuplicates)+len(h.UserDuplicates) > 0 {
		h.Issues++
	}
	if len(h.SystemMissing)+len(h.UserMissing) > 0 {
		h.Issues++
	}
	return h
}

// cleanEntries returns entries without blanks, %PATH% self-references,
// case-insensitive duplicates and directories that no longer exist.
func cleanEntries(entries []string, exists func(string) bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" || strings.EqualFold(e, "%PATH%") {
			continue
		}
		k := strings.ToLower(strings.TrimRight(e, `\/`))
		if seen[k] {
			continue
		}
		seen[k] = true
		if !exists(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Check diagnoses the current PATH. Only Windows has the cmd.exe limit and a
// machine/user split worth cleaning; elsewhere Supported is false.
func Check() Health {
	if goruntime.GOOS != "windows" {
		return Health{Supported: false, Limit: CmdPathLimit}
	}
	system, _ := platform.GetMachinePATH()
	user, _ := platform.GetUserPATH()
	return analyze(system, user, dirExists)
}

func backup(name string, entries []string) {
	dir := filepath.Join(config.GetDataDir(), "backups")
	os.MkdirAll(dir, 0755)
	f := filepath.Join(dir, fmt.Sprintf("%s-path-%s.txt", name, time.Now().Format("20060102-150405")))
	os.WriteFile(f, []byte(strings.Join(entries, "\n")+"\n"), 0644)
}

// CleanUser rewrites the user PATH without duplicates, dead directories and
// %PATH% self-references. Returns how many entries were dropped.
func CleanUser() (int, error) {
	entries, err := platform.GetUserPATH()
	if err != nil {
		return 0, err
	}
	cleaned := cleanEntries(entries, dirExists)
	if len(cleaned) == len(entries) {
		return 0, nil
	}
	backup("user", entries)
	if err := platform.SetUserPATH(cleaned); err != nil {
		return 0, err
	}
	return len(entries) - len(cleaned), nil
}

// CleanSystem does the same for the machine PATH through an elevated write
// (UAC prompt). A backup of the previous value is kept under <data>/backups.
func CleanSystem() (int, error) {
	if goruntime.GOOS != "windows" {
		return 0, fmt.Errorf("system PATH cleanup is only available on Windows")
	}
	entries, err := platform.GetMachinePATH()
	if err != nil {
		return 0, err
	}
	cleaned := cleanEntries(entries, dirExists)
	if len(cleaned) == len(entries) {
		return 0, nil
	}
	backup("system", entries)
	if err := platform.SetMachinePATHElevated(cleaned); err != nil {
		return 0, err
	}
	return len(entries) - len(cleaned), nil
}
