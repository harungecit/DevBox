package pathenv

import (
	"strings"
	"testing"
)

func TestAnalyzeAndClean(t *testing.T) {
	exists := func(p string) bool { return !strings.Contains(strings.ToLower(p), "gone") }
	system := []string{
		`C:\Windows\system32`, `C:\Windows`, `C:\tools\bin`, `%PATH%`, `c:\tools\bin\`, `C:\gone\dir`, ``,
	}
	user := []string{`C:\DevBox\runtimes\php\8.4.25`, `C:\DevBox\runtimes\php\8.4.25`, `C:\gone\user`}

	h := analyze(system, user, exists)
	if !h.Supported || !h.LiteralPath {
		t.Fatalf("literal %%PATH%% not detected: %+v", h)
	}
	if h.SystemEntries != 7 || h.SystemUnique != 4 || len(h.SystemDuplicates) != 1 || len(h.SystemMissing) != 1 {
		t.Fatalf("system analysis: %+v", h)
	}
	if h.UserUnique != 2 || len(h.UserDuplicates) != 1 || len(h.UserMissing) != 1 {
		t.Fatalf("user analysis: %+v", h)
	}
	if h.SystemAfter != 3 || h.UserAfter != 1 || h.Issues != 3 {
		t.Fatalf("after counts/issues: %+v", h)
	}
	if h.TooLong {
		t.Fatal("short PATH flagged as too long")
	}

	long := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		long = append(long, `C:\Program Files\Some Vendor\Product `+strings.Repeat("x", 10)+`\bin`)
	}
	if !analyze(long, nil, func(string) bool { return true }).TooLong {
		t.Fatal("long PATH not flagged")
	}

	cleaned := cleanEntries(system, exists)
	want := []string{`C:\Windows\system32`, `C:\Windows`, `C:\tools\bin`}
	if strings.Join(cleaned, ";") != strings.Join(want, ";") {
		t.Fatalf("cleanEntries = %v, want %v", cleaned, want)
	}
}
