package modules

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	goruntime "runtime"
	"strings"

	"DevBox/internal/platform"

	lua "github.com/yuin/gopher-lua"
)

// installOSOverrides replaces the Lua functions that would misbehave inside a
// desktop app: os.execute / io.popen (visible consoles, lost output) and
// os.exit (would terminate DevBox).
func installOSOverrides(L *lua.LState, h *Host) {
	osTable, _ := L.GetGlobal("os").(*lua.LTable)
	if osTable != nil {
		L.SetField(osTable, "execute", L.NewFunction(func(L *lua.LState) int {
			if L.GetTop() == 0 {
				L.Push(lua.LNumber(1)) // "a shell is available"
				return 1
			}
			code, _ := h.runShell(L, L.CheckString(1), true)
			L.Push(lua.LNumber(code))
			return 1
		}))
		L.SetField(osTable, "exit", L.NewFunction(func(L *lua.LState) int {
			L.RaiseError("os.exit is not available inside DevBox")
			return 0
		}))
	}
	L.SetGlobal("__devbox_popen", L.NewFunction(func(L *lua.LState) int {
		code, out := h.runShell(L, L.CheckString(1), false)
		L.Push(lua.LString(out))
		L.Push(lua.LNumber(code))
		return 2
	}))
}

// lineWriter forwards complete lines to a callback while passing bytes through.
type lineWriter struct {
	buf bytes.Buffer
	fn  func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w.fn == nil {
		return len(p), nil
	}
	w.buf.Write(p)
	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf.Next(i+1)), "\r\n")
		if strings.TrimSpace(line) != "" {
			w.fn(line)
		}
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if w.fn != nil && w.buf.Len() > 0 {
		if line := strings.TrimSpace(w.buf.String()); line != "" {
			w.fn(line)
		}
		w.buf.Reset()
	}
}

// runShell runs a command line through the platform shell with a hidden
// window, the host's cwd/env and output routing. When capture is false the
// output only goes to the log (os.execute); otherwise stdout is returned
// (io.popen). The Lua context's deadline, if any, kills the process tree.
func (h *Host) runShell(L *lua.LState, cmdline string, _ bool) (int, string) {
	ctx := L.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	var cmd *exec.Cmd
	if goruntime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, platform.ComSpec(), "/c", cmdline)
	} else {
		cmd = exec.CommandContext(ctx, platform.ComSpec(), "-c", cmdline)
	}
	platform.SetProcessAttrs(cmd, true, true)
	if goruntime.GOOS == "windows" {
		setRawCmdLine(cmd, cmdline)
	}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return platform.KillProcessTree(cmd.Process.Pid)
		}
		return nil
	}

	h.mu.Lock()
	cmd.Dir = h.Dir
	if h.Env != nil {
		cmd.Env = h.Env
	}
	logW := h.Log
	onOut := h.OnOutput
	h.mu.Unlock()

	var stdout bytes.Buffer
	lw := &lineWriter{fn: onOut}
	writers := []io.Writer{&stdout, lw}
	if logW != nil {
		writers = append(writers, logW)
	}
	errWriters := []io.Writer{lw}
	if logW != nil {
		errWriters = append(errWriters, logW)
	}
	cmd.Stdout = io.MultiWriter(writers...)
	cmd.Stderr = io.MultiWriter(errWriters...)
	cmd.Stdin = nil

	if logW != nil {
		bw := bufio.NewWriter(logW)
		bw.WriteString("$ " + cmdline + "\n")
		bw.Flush()
	}

	h.mu.Lock()
	h.running = cmd
	h.mu.Unlock()
	err := cmd.Run()
	h.mu.Lock()
	h.running = nil
	h.mu.Unlock()
	lw.flush()

	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
			if logW != nil {
				io.WriteString(logW, "! "+err.Error()+"\n")
			}
		}
		if code == 0 {
			code = 1
		}
	}
	return code, stdout.String()
}

// KillRunning stops whatever shell command the plugin is currently running.
func (h *Host) KillRunning() {
	h.mu.Lock()
	cmd := h.running
	h.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = platform.KillProcessTree(cmd.Process.Pid)
	}
}
