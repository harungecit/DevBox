package runtime

import (
	"os"
	"path/filepath"
	"regexp"

	"DevBox/internal/platform"
)

// PluginProbe describes how to recognise an external installation of a
// plugin runtime: the executable to look for and how to ask it for its
// version. Used by discovery (Import Center) and ImportExternal.
type PluginProbe struct {
	Exe     string
	VerArgs []string
	VerRe   *regexp.Regexp
}

var genericVersionRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// pluginProbes covers the well-known registry plugins; anything else is
// probed as "<plugin name> --version".
var pluginProbes = map[string]PluginProbe{
	"java":   {Exe: "java", VerArgs: []string{"-version"}, VerRe: regexp.MustCompile(`version "(\d+(?:\.\d+)*)`)},
	"dotnet": {Exe: "dotnet", VerArgs: []string{"--version"}},
	"ruby":   {Exe: "ruby", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`ruby (\d+\.\d+\.\d+)`)},
	"deno":   {Exe: "deno", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`deno (\d+\.\d+\.\d+)`)},
	"bun":    {Exe: "bun", VerArgs: []string{"--version"}},
	"dart":   {Exe: "dart", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Dart SDK version: (\d+\.\d+\.\d+)`)},
	"flutter": {Exe: "flutter", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Flutter (\d+\.\d+\.\d+)`)},
	"kotlin": {Exe: "kotlinc", VerArgs: []string{"-version"}, VerRe: regexp.MustCompile(`kotlinc-jvm (\d+\.\d+\.\d+)`)},
	"zig":    {Exe: "zig", VerArgs: []string{"version"}},
	"crystal": {Exe: "crystal", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Crystal (\d+\.\d+\.\d+)`)},
	"elixir": {Exe: "elixir", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Elixir (\d+\.\d+\.\d+)`)},
	"erlang": {Exe: "erl", VerArgs: []string{"-noshell", "-eval", `io:format("~s", [erlang:system_info(otp_release)]), halt().`}, VerRe: regexp.MustCompile(`(\d+)`)},
	"julia":  {Exe: "julia", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`julia version (\d+\.\d+\.\d+)`)},
	"lua":    {Exe: "lua", VerArgs: []string{"-v"}, VerRe: regexp.MustCompile(`Lua (\d+\.\d+(?:\.\d+)?)`)},
	"scala":  {Exe: "scala", VerArgs: []string{"-version"}},
	"gradle": {Exe: "gradle", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Gradle (\d+\.\d+(?:\.\d+)?)`)},
	"maven":  {Exe: "mvn", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`Apache Maven (\d+\.\d+\.\d+)`)},
	"terraform": {Exe: "terraform", VerArgs: []string{"version"}, VerRe: regexp.MustCompile(`v(\d+\.\d+\.\d+)`)},
	"kubectl": {Exe: "kubectl", VerArgs: []string{"version", "--client"}, VerRe: regexp.MustCompile(`v(\d+\.\d+\.\d+)`)},
	"cmake":  {Exe: "cmake", VerArgs: []string{"--version"}, VerRe: regexp.MustCompile(`cmake version (\d+\.\d+\.\d+)`)},
	"vlang":  {Exe: "v", VerArgs: []string{"version"}},
	"typst":  {Exe: "typst", VerArgs: []string{"--version"}},
}

// ProbeFor returns the probe of a plugin runtime (a generic one for unknown names).
func ProbeFor(name string) PluginProbe {
	p, ok := pluginProbes[name]
	if !ok {
		p = PluginProbe{Exe: name, VerArgs: []string{"--version"}}
	}
	if p.VerRe == nil {
		p.VerRe = genericVersionRe
	}
	return p
}

// PluginBinaryInRoot returns the plugin runtime's executable inside an
// installation root, looking under bin/ first, then the root itself.
func PluginBinaryInRoot(name, root string) string {
	exe := platform.BinaryName(ProbeFor(name).Exe)
	for _, cand := range []string{filepath.Join(root, "bin", exe), filepath.Join(root, exe)} {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	return ""
}
