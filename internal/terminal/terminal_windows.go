//go:build windows

package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const createNewConsole = 0x00000010

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if exists(p) {
			return p
		}
	}
	return ""
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// detect lists Windows terminals in preference order.
func detect() []Terminal {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LOCALAPPDATA")

	wt := firstExisting(filepath.Join(local, "Microsoft", "WindowsApps", "wt.exe"), lookPath("wt.exe"))
	pwsh := firstExisting(filepath.Join(pf, "PowerShell", "7", "pwsh.exe"), lookPath("pwsh.exe"))
	posh := firstExisting(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), lookPath("powershell.exe"))
	cmd := firstExisting(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), lookPath("cmd.exe"))
	gitRoot := ""
	for _, r := range []string{filepath.Join(pf, "Git"), filepath.Join(pf86, "Git"), filepath.Join(local, "Programs", "Git")} {
		if exists(filepath.Join(r, "git-bash.exe")) {
			gitRoot = r
			break
		}
	}
	cmder := ""
	if root := os.Getenv("CMDER_ROOT"); root != "" && exists(filepath.Join(root, "Cmder.exe")) {
		cmder = root
	} else if p := lookPath("Cmder.exe"); p != "" {
		cmder = filepath.Dir(p)
	}
	conemu := firstExisting(filepath.Join(pf, "ConEmu", "ConEmu64.exe"), filepath.Join(pf86, "ConEmu", "ConEmu64.exe"))

	// Windows Terminal hosts the best shell available.
	wtShell, wtShellName := pwsh, "pwsh"
	if wtShell == "" {
		wtShell, wtShellName = posh, "powershell"
	}

	return []Terminal{
		{ID: "wt", Name: "Windows Terminal", Shell: wtShellName, Path: wt, Installed: wt != "" && wtShell != ""},
		{ID: "pwsh", Name: "PowerShell 7", Shell: "pwsh", Path: pwsh, Installed: pwsh != ""},
		{ID: "gitbash", Name: "Git Bash", Shell: "bash", Path: gitRoot, Installed: gitRoot != ""},
		{ID: "cmder", Name: "Cmder", Shell: "cmd", Path: cmder, Installed: cmder != ""},
		{ID: "conemu", Name: "ConEmu", Shell: wtShellName, Path: conemu, Installed: conemu != ""},
		{ID: "powershell", Name: "Windows PowerShell", Shell: "powershell", Path: posh, Installed: posh != ""},
		{ID: "cmd", Name: "Command Prompt", Shell: "cmd", Path: cmd, Installed: cmd != ""},
	}
}

// shellArgs returns the shell executable + args that run the init script and
// then stay interactive.
func shellArgs(shell, script string) (string, []string) {
	switch shell {
	case "pwsh":
		return firstExisting(filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe"), lookPath("pwsh.exe")),
			[]string{"-NoLogo", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", script}
	case "powershell":
		return "powershell.exe", []string{"-NoLogo", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", script}
	default:
		return "cmd.exe", []string{"/k", script}
	}
}

func launch(t Terminal, s Session, script string) error {
	var cmd *exec.Cmd
	switch t.ID {
	case "wt":
		exe, args := shellArgs(t.Shell, script)
		cmd = exec.Command(t.Path, append([]string{"-d", s.Dir, exe}, args...)...)
	case "gitbash":
		// mintty + bash with our rc file: the user's ~/.bashrc is sourced from it.
		mintty := filepath.Join(t.Path, "usr", "bin", "mintty.exe")
		bash := filepath.Join(t.Path, "usr", "bin", "bash.exe")
		if exists(mintty) && exists(bash) {
			cmd = exec.Command(mintty, "-t", "DevBox · "+s.Title, "-e", bash, "--rcfile", toPosixPath(script), "-i")
		} else {
			cmd = exec.Command(filepath.Join(t.Path, "git-bash.exe"), "--cd="+s.Dir)
		}
	case "cmder":
		conemu := filepath.Join(t.Path, "vendor", "conemu-maximus5", "ConEmu64.exe")
		exe, args := shellArgs("cmd", script)
		if exists(conemu) {
			cmd = exec.Command(conemu, append([]string{"-Icon", filepath.Join(t.Path, "icons", "cmder.ico"), "-Dir", s.Dir, "-run", exe}, args...)...)
		} else {
			cmd = exec.Command(filepath.Join(t.Path, "Cmder.exe"), "/START", s.Dir)
		}
	case "conemu":
		exe, args := shellArgs(t.Shell, script)
		cmd = exec.Command(t.Path, append([]string{"-Dir", s.Dir, "-run", exe}, args...)...)
	default: // pwsh / powershell / cmd in a fresh console window
		exe, args := shellArgs(t.Shell, script)
		cmd = exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	}
	cmd.Dir = s.Dir
	cmd.Env = os.Environ()
	return cmd.Start()
}
