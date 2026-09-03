//go:build !windows

package modules

import "os/exec"

// setRawCmdLine is only needed on Windows; /bin/sh -c takes the line as one argument.
func setRawCmdLine(cmd *exec.Cmd, cmdline string) {}
