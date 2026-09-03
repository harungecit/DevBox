package vfox

import (
	"strconv"
	"strings"
)

// CompareVersions orders dotted version strings numerically ("1.10" > "1.9",
// a leading "v" is ignored, a pre-release suffix sorts below the release).
// Returns >0 when a is newer, <0 when b is newer, 0 when equal.
func CompareVersions(a, b string) int {
	a, preA := splitPre(strings.TrimPrefix(strings.TrimSpace(a), "v"))
	b, preB := splitPre(strings.TrimPrefix(strings.TrimSpace(b), "v"))
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var na, nb int
		if i < len(pa) {
			na = leadingInt(pa[i])
		}
		if i < len(pb) {
			nb = leadingInt(pb[i])
		}
		if na != nb {
			return na - nb
		}
	}
	switch {
	case preA == "" && preB != "":
		return 1
	case preA != "" && preB == "":
		return -1
	}
	return strings.Compare(preA, preB)
}

func splitPre(v string) (string, string) {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func leadingInt(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// IsPreRelease guesses from the version string whether it is a pre-release.
func IsPreRelease(v string) bool {
	l := strings.ToLower(v)
	for _, m := range []string{"rc", "beta", "alpha", "dev", "nightly", "preview", "snapshot", "canary", "pre"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return strings.Contains(l, "-")
}
