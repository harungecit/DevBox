//go:build windows

package discovery

import (
	"os"
	"path/filepath"
)

// runtimeCandidateRoots lists well-known Windows install locations per runtime.
// Globs are allowed; only existing directories survive expansion.
func runtimeCandidateRoots(name string) []string {
	home, _ := os.UserHomeDir()
	programFiles := envOr("ProgramFiles", `C:\Program Files`)
	localAppData := envOr("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	appData := envOr("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	scoop := filepath.Join(home, "scoop", "apps")

	switch name {
	case "node":
		return []string{
			filepath.Join(programFiles, "nodejs"),
			filepath.Join(appData, "nvm", "v*"),
			filepath.Join(scoop, "nodejs*", "current"),
		}
	case "go":
		return []string{
			filepath.Join(programFiles, "Go"),
			`C:\Go`,
			filepath.Join(localAppData, "Programs", "Go"),
			filepath.Join(scoop, "go", "current"),
		}
	case "php":
		return []string{
			`C:\php*`,
			`C:\tools\php*`,
			`C:\xampp\php`,
			`C:\laragon\bin\php\*`,
			filepath.Join(scoop, "php*", "current"),
		}
	case "python":
		return []string{
			filepath.Join(localAppData, "Programs", "Python", "Python3*"),
			`C:\Python3*`,
			filepath.Join(scoop, "python*", "current"),
		}
	case "rust":
		return []string{
			filepath.Join(home, ".rustup", "toolchains", "*"),
		}
	}
	return nil
}

// serviceCandidateRoots lists well-known Windows install locations per service.
func serviceCandidateRoots(name string) []string {
	home, _ := os.UserHomeDir()
	programFiles := envOr("ProgramFiles", `C:\Program Files`)
	scoop := filepath.Join(home, "scoop", "apps")

	switch name {
	case "nginx":
		return []string{
			`C:\nginx*`,
			`C:\tools\nginx*`,
			`C:\laragon\bin\nginx\*`,
			filepath.Join(scoop, "nginx", "current"),
		}
	case "apache":
		return []string{
			`C:\Apache24`,
			`C:\xampp\apache`,
			`C:\laragon\bin\apache\*`,
		}
	case "caddy":
		return []string{
			filepath.Join(scoop, "caddy", "current"),
		}
	case "postgres":
		return []string{
			filepath.Join(programFiles, "PostgreSQL", "*"),
		}
	case "mysql":
		return []string{
			filepath.Join(programFiles, "MySQL", "MySQL Server *"),
			`C:\xampp\mysql`,
			`C:\wamp64\bin\mysql\*`,
			`C:\laragon\bin\mysql\*`,
		}
	case "mariadb":
		return []string{
			filepath.Join(programFiles, "MariaDB *"),
			`C:\laragon\bin\mysql\mariadb*`,
		}
	case "mongodb":
		return []string{
			filepath.Join(programFiles, "MongoDB", "Server", "*"),
		}
	case "redis":
		return []string{
			filepath.Join(programFiles, "Redis*"),
			`C:\tools\redis*`,
			`C:\laragon\bin\redis\*`,
			filepath.Join(scoop, "redis", "current"),
		}
	case "mailpit":
		return []string{
			filepath.Join(scoop, "mailpit", "current"),
		}
	}
	return nil
}

// composerCandidatePhars lists well-known Windows Composer locations. Entries
// may be the phar itself or a wrapper that names it; globs are allowed.
func composerCandidatePhars() []string {
	home, _ := os.UserHomeDir()
	appData := envOr("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	programData := envOr("ProgramData", `C:\ProgramData`)
	return []string{
		filepath.Join(programData, "ComposerSetup", "bin", "composer.phar"),
		filepath.Join(appData, "Composer", "composer.phar"),
		`C:\laragon\bin\composer\composer.phar`,
		`C:\xampp\php\composer.phar`,
		`C:\wamp64\bin\composer\composer.phar`,
		`C:\composer\composer.phar`,
		filepath.Join(home, "scoop", "apps", "composer", "current", "composer.phar"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
