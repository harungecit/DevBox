//go:build windows

package modules

import (
	"os/exec"
	"syscall"
)

// setRawCmdLine hands cmd.exe the command line verbatim. Go would otherwise
// re-quote the /c argument, which breaks redirections and quoted paths.
func setRawCmdLine(cmd *exec.Cmd, cmdline string) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = `"` + cmd.Path + `" /c "` + cmdline + `"`
}
