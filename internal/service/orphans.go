package service

import (
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"

	"DevBox/internal/platform"
)

// killOrphanedProcesses terminates every running process whose executable
// lives under baseDir and is named exeName — i.e. children DevBox lost track
// of (worker processes that outlived their master, or a service left behind by
// a crashed session). Only processes inside DevBox's own directory are touched,
// so a user's separately installed nginx/mysql is never affected.
// Returns the number of processes killed.
func killOrphanedProcesses(baseDir, exeName string) int {
	var pids []int
	switch goruntime.GOOS {
	case "windows":
		// WMI gives the executable path; filter on our directory.
		ps := `Get-CimInstance Win32_Process -Filter "name='` + exeName + `'" | ForEach-Object { "$($_.ProcessId)|$($_.ExecutablePath)" }`
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		platform.SetProcessAttrs(cmd, false, true)
		out, err := cmd.Output()
		if err != nil {
			return 0
		}
		prefix := strings.ToLower(baseDir)
		for _, line := range strings.Split(string(out), "\n") {
			pidStr, path, ok := strings.Cut(strings.TrimSpace(line), "|")
			if !ok || !strings.HasPrefix(strings.ToLower(path), prefix) {
				continue
			}
			if pid, err := strconv.Atoi(pidStr); err == nil {
				pids = append(pids, pid)
			}
		}
		for _, pid := range pids {
			kill := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
			platform.SetProcessAttrs(kill, false, true)
			kill.Run()
		}
	default:
		cmd := exec.Command("pgrep", "-f", baseDir+"/")
		out, err := cmd.Output()
		if err != nil {
			return 0
		}
		for _, f := range strings.Fields(string(out)) {
			if pid, err := strconv.Atoi(f); err == nil {
				pids = append(pids, pid)
				exec.Command("kill", "-9", f).Run()
			}
		}
	}
	return len(pids)
}
