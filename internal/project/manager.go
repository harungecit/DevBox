package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"DevBox/internal/config"
)

// Project represents a registered development project
type Project struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Domain       string `json:"domain"`       // e.g., "my-app.test"
	Framework    string `json:"framework"`     // e.g., "Laravel", "Next.js", "Go"
	SSL          bool   `json:"ssl"`
	Port         int    `json:"port"`         // app dev server port (0 = served by web server for PHP/Static)
	StartCommand string `json:"startCommand"` // custom start command (empty = auto-detect)
}

// IsAppServer returns true if the framework runs its own HTTP server (Node, Python, Go, Rust etc.)
func IsAppServer(framework string) bool {
	switch framework {
	case "Laravel", "WordPress", "Symfony", "PHP", "Static", "":
		return false
	}
	return true
}

// DefaultPort returns the default dev server port for a framework
func DefaultPort(framework string) int {
	switch framework {
	case "Next.js":
		return 3000
	case "Nuxt":
		return 3000
	case "Vue", "React", "Svelte", "Angular":
		return 5173
	case "Django":
		return 8000
	case "Python":
		return 8000
	case "Go":
		return 8080
	case "Rust":
		return 8080
	}
	return 0
}

// projectsFilePath returns the path to projects.json
func projectsFilePath() string {
	return filepath.Join(config.GetDataDir(), "projects.json")
}

// ListProjects reads all projects from projects.json
func ListProjects() ([]Project, error) {
	data, err := os.ReadFile(projectsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Project{}, nil
		}
		return nil, err
	}

	var projects []Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// SaveProjects writes all projects to projects.json
func SaveProjects(projects []Project) error {
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(projectsFilePath(), data, 0644)
}

// AddProject adds a new project from a folder path
func AddProject(projectPath, domain string) (*Project, error) {
	name := filepath.Base(projectPath)
	if domain == "" {
		domain = strings.ToLower(name) + ".test"
	}

	// Clean domain
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !strings.HasSuffix(domain, ".test") && !strings.HasSuffix(domain, ".local") {
		domain = domain + ".test"
	}

	framework := DetectFramework(projectPath)

	project := Project{
		Name:      name,
		Path:      projectPath,
		Domain:    domain,
		Framework: framework,
		SSL:       false,
		Port:      DefaultPort(framework),
	}

	projects, err := ListProjects()
	if err != nil {
		projects = []Project{}
	}

	// Check for duplicates
	for _, p := range projects {
		if p.Path == projectPath {
			return &p, nil
		}
	}

	projects = append(projects, project)
	if err := SaveProjects(projects); err != nil {
		return nil, err
	}

	return &project, nil
}

// RemoveProject removes a project by name
func RemoveProject(name string) error {
	projects, err := ListProjects()
	if err != nil {
		return err
	}

	var filtered []Project
	for _, p := range projects {
		if p.Name != name {
			filtered = append(filtered, p)
		}
	}

	return SaveProjects(filtered)
}

// DetectFramework detects the project framework by checking marker files
func DetectFramework(projectPath string) string {
	// Laravel
	if fileExists(filepath.Join(projectPath, "artisan")) &&
		fileExists(filepath.Join(projectPath, "composer.json")) {
		return "Laravel"
	}

	// WordPress
	if fileExists(filepath.Join(projectPath, "wp-config.php")) ||
		fileExists(filepath.Join(projectPath, "wp-config-sample.php")) {
		return "WordPress"
	}

	// Symfony
	if fileExists(filepath.Join(projectPath, "symfony.lock")) {
		return "Symfony"
	}

	// Next.js
	if fileExists(filepath.Join(projectPath, "next.config.js")) ||
		fileExists(filepath.Join(projectPath, "next.config.mjs")) ||
		fileExists(filepath.Join(projectPath, "next.config.ts")) {
		return "Next.js"
	}

	// Nuxt
	if fileExists(filepath.Join(projectPath, "nuxt.config.js")) ||
		fileExists(filepath.Join(projectPath, "nuxt.config.ts")) {
		return "Nuxt"
	}

	// Vue
	if fileExists(filepath.Join(projectPath, "vue.config.js")) ||
		fileExists(filepath.Join(projectPath, "vite.config.ts")) {
		return "Vue"
	}

	// React (CRA)
	if fileExists(filepath.Join(projectPath, "package.json")) {
		data, _ := os.ReadFile(filepath.Join(projectPath, "package.json"))
		if strings.Contains(string(data), "\"react\"") {
			return "React"
		}
		if strings.Contains(string(data), "\"svelte\"") {
			return "Svelte"
		}
		if strings.Contains(string(data), "\"@angular/core\"") {
			return "Angular"
		}
	}

	// Go
	if fileExists(filepath.Join(projectPath, "go.mod")) {
		return "Go"
	}

	// Rust
	if fileExists(filepath.Join(projectPath, "Cargo.toml")) {
		return "Rust"
	}

	// Python (Django / Flask)
	if fileExists(filepath.Join(projectPath, "manage.py")) {
		return "Django"
	}
	if fileExists(filepath.Join(projectPath, "requirements.txt")) {
		return "Python"
	}

	// PHP (generic)
	if fileExists(filepath.Join(projectPath, "composer.json")) {
		return "PHP"
	}

	// Static HTML
	if fileExists(filepath.Join(projectPath, "index.html")) {
		return "Static"
	}

	return ""
}

// UpdateProjectDomain updates domain for a project
func UpdateProjectDomain(name, domain string) error {
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].Domain = domain
			return SaveProjects(projects)
		}
	}
	return nil
}

// SetProjectSSL marks a project as having SSL
func SetProjectSSL(name string, ssl bool) error {
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].SSL = ssl
			return SaveProjects(projects)
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
