// Package modules provides the Lua standard modules vfox plugins expect
// (http, json, html, strings/vfox.strings, archiver, file) plus DevBox's
// replacements for os.execute / io.popen / os.exit, so plugin hooks run
// inside a GUI process without popping console windows or killing the app.
package modules

import (
	_ "embed"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

//go:embed preload.lua
var preloadScript string

// Host carries the per-plugin (and per-install) state the modules need:
// where shell commands run, with which environment, and where their output goes.
type Host struct {
	mu sync.Mutex

	// Dir is the working directory for os.execute / io.popen children.
	Dir string
	// Env is the child environment (nil = inherit the process environment).
	Env []string
	// Log receives everything commands print (nil = discarded).
	Log io.Writer
	// OnOutput is called per output line (progress reporting).
	OnOutput func(line string)
	// Decompress backs archiver.decompress.
	Decompress func(src, dest string) error
	// SymlinkRoot is joined onto both arguments of file.symlink, as vfox does.
	SymlinkRoot string
	// HTTPClient serves the http module (nil = DefaultHTTPClient()).
	HTTPClient *http.Client

	running *exec.Cmd
}

// DefaultHTTPClient is the client plugins use when the host sets none:
// no overall timeout (release archives can be huge) but headers must arrive
// within 30s, and proxies come from the environment.
func DefaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

func (h *Host) client() *http.Client {
	if h.HTTPClient != nil {
		return h.HTTPClient
	}
	return DefaultHTTPClient()
}

// SetExecContext points shell commands at dir/env and routes their output.
func (h *Host) SetExecContext(dir string, env []string, log io.Writer, onOutput func(string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Dir, h.Env, h.Log, h.OnOutput = dir, env, log, onOutput
}

// Preload registers every module and the os/io overrides on L. Call it once
// on a fresh state before loading plugin scripts.
func Preload(L *lua.LState, h *Host) error {
	preloadHTTP(L, h)
	preloadJSON(L)
	preloadHTML(L)
	preloadStrings(L)
	preloadArchiver(L, h)
	preloadFile(L, h)
	installOSOverrides(L, h)
	return L.DoString(preloadScript)
}
