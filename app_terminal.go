package main

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"

	"DevBox/internal/config"
	"DevBox/internal/platform"
	"DevBox/internal/project"
	"DevBox/internal/runtime"
	"DevBox/internal/service"
	"DevBox/internal/terminal"
)

// --- Terminal integration ---
//
// DevBox opens the user's terminal with the right environment instead of
// embedding one: project terminals get the project's pinned runtime on PATH,
// service terminals open the matching CLI already connected, the general
// terminal has every global runtime and tool on PATH.

// ListTerminals returns the terminals detected on this machine (preferred first).
func (a *App) ListTerminals() []terminal.Terminal {
	return terminal.List()
}

// SetTerminal stores the preferred terminal id ("" = auto).
func (a *App) SetTerminal(id string) error {
	cfg := config.Get()
	cfg.Terminal = id
	return config.Save()
}

// devboxPathDirs lists every DevBox-managed bin dir for the general terminal:
// global runtimes first, then tools (bun, uv, go/cargo installs…).
func devboxPathDirs() []string {
	dirs := runtime.ManagedPathDirs()
	tools := filepath.Join(config.GetDataDir(), "tools")
	for _, sub := range []string{"bun", "composer", "uv", "gobin", filepath.Join("cargo", "bin"), "mkcert", "cloudflared"} {
		if d := filepath.Join(tools, sub); dirExists(d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// devboxEnvVars adds the variables plugin runtimes' global versions need
// (JAVA_HOME, GOROOT...) to base without overriding what is already there.
func devboxEnvVars(base map[string]string) map[string]string {
	for k, v := range runtime.ManagedEnvVars() {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return base
}

// OpenTerminal opens the general DevBox terminal in the projects folder.
func (a *App) OpenTerminal() error {
	dir := filepath.Join(config.GetDataDir(), "projects")
	if !dirExists(dir) {
		dir = config.GetDataDir()
	}
	return terminal.Open(terminal.Session{
		Title: "devbox",
		Dir:   dir,
		Path:  devboxPathDirs(),
		Env:   devboxEnvVars(map[string]string{"DEVBOX_HOME": config.GetDataDir()}),
	})
}

// OpenProjectTerminal opens a terminal in the project folder with its pinned
// runtime first on PATH.
func (a *App) OpenProjectTerminal(name string) error {
	projects, err := project.ListProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.Name != name {
			continue
		}
		var dirs []string
		env := map[string]string{
			"DEVBOX_PROJECT": p.Name,
			"DEVBOX_HOME":    config.GetDataDir(),
		}
		if mgr, ok := runtime.Registry[p.Runtime]; ok {
			if ver := project.ResolveRuntimeVersion(p); ver != "" {
				dirs = append(dirs, runtime.ActivationPaths(mgr, ver)...)
				if p.Runtime == "python" && goruntime.GOOS == "windows" {
					dirs = append(dirs, filepath.Join(mgr.BinaryPath(ver), "Scripts"))
				}
				for k, v := range runtime.ActivationVars(mgr, ver) {
					env[k] = v // the pinned version wins over the global one
				}
			}
		}
		dirs = append(dirs, devboxPathDirs()...)
		env = devboxEnvVars(env)
		if p.Domain != "" {
			env["DEVBOX_DOMAIN"] = p.Domain
		}
		if project.IsAppServer(p.Framework) && p.Port > 0 {
			env["PORT"] = strconv.Itoa(p.Port)
		}
		return terminal.Open(terminal.Session{Title: p.Name, Dir: p.Path, Path: dedupe(dirs), Env: env})
	}
	return fmt.Errorf("project not found: %s", name)
}

// OpenServiceTerminal opens a terminal with the service's CLI connected to
// the DevBox instance (psql, mysql, redis-cli, mongosh…).
func (a *App) OpenServiceTerminal(name string) error {
	mgr, ok := service.Registry[name]
	if !ok || !mgr.IsInstalled() {
		return fmt.Errorf("%s is not installed", name)
	}
	base := filepath.Join(config.GetDataDir(), "services", name)
	port := strconv.Itoa(mgr.Port())
	user, pass := service.Credentials(name)
	s := terminal.Session{Title: name, Dir: base, Env: devboxEnvVars(map[string]string{"DEVBOX_HOME": config.GetDataDir()})}

	bin := filepath.Join(base, "bin")
	switch name {
	case "postgres":
		s.Path = []string{bin}
		s.Env["PGHOST"], s.Env["PGPORT"], s.Env["PGUSER"] = "127.0.0.1", port, user
		if pass != "" {
			s.Env["PGPASSWORD"] = pass
		}
		s.Cmd = "psql -d postgres"
	case "mysql", "mariadb":
		s.Path = []string{bin}
		if pass != "" {
			s.Env["MYSQL_PWD"] = pass
		}
		cli := "mysql"
		if name == "mariadb" && fileExists(filepath.Join(bin, platform.BinaryName("mariadb"))) {
			cli = "mariadb"
		}
		s.Cmd = fmt.Sprintf("%s -u %s -h 127.0.0.1 -P %s --protocol=tcp", cli, user, port)
	case "redis", "valkey":
		s.Path = []string{base}
		cli := name + "-cli"
		s.Cmd = fmt.Sprintf("%s -h 127.0.0.1 -p %s", cli, port)
		if pass != "" {
			s.Env["REDISCLI_AUTH"] = pass
		}
	case "mongodb":
		s.Path = []string{bin}
		if fileExists(filepath.Join(bin, platform.BinaryName("mongosh"))) {
			s.Cmd = "mongosh --host 127.0.0.1 --port " + port
		}
	default:
		s.Path = []string{bin, base}
	}
	s.Path = append(s.Path, devboxPathDirs()...)
	s.Path = dedupe(s.Path)
	if mgr.Status() != service.StatusRunning && s.Cmd != "" {
		// Bring the service up so the CLI connects instead of erroring out.
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("could not start %s: %w", name, err)
		}
		a.emitServicesChanged()
	}
	return terminal.Open(s)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range in {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
