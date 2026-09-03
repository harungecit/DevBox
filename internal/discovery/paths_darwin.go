//go:build darwin

package discovery

import (
	"os"
	"path/filepath"
)

// brewPrefixes returns the Homebrew opt directories for both Apple Silicon
// and Intel layouts. `opt/<formula>` is a stable symlink into the Cellar.
func brewPrefixes() []string {
	return []string{"/opt/homebrew/opt", "/usr/local/opt"}
}

func brewOpt(formulaGlobs ...string) []string {
	var out []string
	for _, prefix := range brewPrefixes() {
		for _, g := range formulaGlobs {
			out = append(out, filepath.Join(prefix, g))
		}
	}
	return out
}

// runtimeCandidateRoots lists well-known macOS install locations per runtime.
func runtimeCandidateRoots(name string) []string {
	home, _ := os.UserHomeDir()
	switch name {
	case "node":
		roots := brewOpt("node", "node@*")
		roots = append(roots, filepath.Join(home, ".nvm", "versions", "node", "v*"))
		return roots
	case "go":
		return append(brewOpt("go"), "/usr/local/go")
	case "php":
		return brewOpt("php", "php@*")
	case "python":
		return brewOpt("python@3.*")
	case "rust":
		return []string{filepath.Join(home, ".rustup", "toolchains", "*")}
	}
	return nil
}

// serviceCandidateRoots lists well-known macOS install locations per service.
func serviceCandidateRoots(name string) []string {
	switch name {
	case "nginx":
		return brewOpt("nginx")
	case "apache":
		return brewOpt("httpd")
	case "caddy":
		return brewOpt("caddy")
	case "postgres":
		return brewOpt("postgresql@*")
	case "mysql":
		return brewOpt("mysql", "mysql@*")
	case "mariadb":
		return brewOpt("mariadb", "mariadb@*")
	case "mongodb":
		return brewOpt("mongodb-community*")
	case "redis":
		return brewOpt("redis")
	case "valkey":
		return brewOpt("valkey")
	case "mailpit":
		return brewOpt("mailpit")
	}
	return nil
}

// composerCandidatePhars lists well-known macOS Composer locations (the
// Homebrew formula installs the phar itself as bin/composer).
func composerCandidatePhars() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/opt/homebrew/bin/composer",
		"/usr/local/bin/composer",
		"/usr/local/bin/composer.phar",
		filepath.Join(home, ".composer", "composer.phar"),
		filepath.Join(home, "composer.phar"),
	}
}
