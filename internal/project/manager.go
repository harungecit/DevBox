package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"DevBox/internal/config"
)

// Project represents a registered development project
type Project struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Domain       string `json:"domain"`       // e.g., "my-app.test"
	Framework    string `json:"framework"`    // e.g., "Laravel", "Next.js", "Go"
	SSL          bool   `json:"ssl"`
	Port         int    `json:"port"`         // app dev server port (0 = served by web server for PHP/Static)
	StartCommand string `json:"startCommand"` // custom start command (empty = auto-detect)

	// Runtime is the language/category the project is built on. Independent of
	// Framework so it survives framework misdetection. Derived from Framework if
	// empty during ListProjects (backward-compat migration for older entries).
	// Values: "php" | "node" | "go" | "python" | "rust" | "static".
	Runtime string `json:"runtime,omitempty"`

	// RuntimeVersion pins the project to a specific runtime version. Empty means
	// "use the globally active runtime version" (current default behavior).
	RuntimeVersion string `json:"runtimeVersion,omitempty"`

	// Webserver selects how the front-door routes this project. Empty means
	// "auto" — derived from Runtime: php → primary installed web server,
	// node/go/python/rust → "devserver" (the project's own dev server),
	// static → primary web server (file server).
	// Explicit values: "nginx" | "caddy" | "apache" | "frankenphp" | "devserver".
	Webserver string `json:"webserver,omitempty"`

	// PublicHostname is the hostname on the user's own Cloudflare zone that a
	// named tunnel exposes this project at (e.g. "myapp.example.com"). Empty
	// means "share via a random *.trycloudflare.com quick tunnel".
	PublicHostname string `json:"publicHostname,omitempty"`
}

// RuntimeFromFramework maps a detected framework name to the underlying runtime.
// Returns "" for unrecognized frameworks.
func RuntimeFromFramework(framework string) string {
	switch framework {
	case "Laravel", "WordPress", "Symfony", "PHP":
		return "php"
	case "Next.js", "Nuxt", "Vue", "React", "Svelte", "Angular":
		return "node"
	case "Django", "Python":
		return "python"
	case "Go":
		return "go"
	case "Rust":
		return "rust"
	case "Static":
		return "static"
	}
	return ""
}

// DefaultWebserverForRuntime returns the resolved webserver choice when a project's
// Webserver field is empty ("auto"). PHP/Static get served by a real web server;
// app-server runtimes use their own dev server.
func DefaultWebserverForRuntime(rt string) string {
	switch rt {
	case "php", "static":
		return "auto" // resolved later against installed webservers (nginx/caddy/apache)
	case "node", "go", "python", "rust":
		return "devserver"
	}
	return "auto"
}

// fillDefaults populates derived fields (Runtime, Webserver) for a project that
// was saved before these fields existed. Mutates in place.
func (p *Project) fillDefaults() {
	if p.Runtime == "" {
		p.Runtime = RuntimeFromFramework(p.Framework)
	}
	if p.Webserver == "" {
		p.Webserver = DefaultWebserverForRuntime(p.Runtime)
	}
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

// ListProjects reads all projects from projects.json. Missing Runtime/Webserver
// fields on legacy entries are filled in-memory so callers always see a
// fully-populated Project.
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
	for i := range projects {
		projects[i].fillDefaults()
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
	rt := RuntimeFromFramework(framework)

	project := Project{
		Name:      name,
		Path:      projectPath,
		Domain:    domain,
		Framework: framework,
		SSL:       false,
		Port:      DefaultPort(framework),
		Runtime:   rt,
		Webserver: DefaultWebserverForRuntime(rt),
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

// validRuntimes / validWebservers gate the public setters so the persisted
// values stay within the known vocabulary the rest of DevBox expects.
var validRuntimes = map[string]bool{
	"php": true, "node": true, "go": true, "python": true, "rust": true, "static": true,
}
var validWebservers = map[string]bool{
	"auto": true, "nginx": true, "caddy": true, "apache": true, "frankenphp": true, "devserver": true,
}

// SetProjectRuntime overrides a project's runtime. Empty string resets to
// auto-derived-from-Framework behavior.
func SetProjectRuntime(name, rt string) error {
	if rt != "" && !validRuntimes[rt] {
		return fmt.Errorf("invalid runtime %q", rt)
	}
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].Runtime = rt
			// Reset webserver to "auto" if the runtime changed — the previous
			// choice may no longer be sensible (e.g. nginx for a Go runtime).
			projects[i].Webserver = ""
			return SaveProjects(projects)
		}
	}
	return nil
}

// SetProjectRuntimeVersion pins a project to a specific runtime version, or
// clears the pin (empty string = use global active version).
func SetProjectRuntimeVersion(name, version string) error {
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].RuntimeVersion = version
			return SaveProjects(projects)
		}
	}
	return nil
}

// SetProjectPublicHostname sets (or clears) the custom tunnel hostname.
func SetProjectPublicHostname(name, hostname string) error {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname != "" {
		if matched, _ := regexp.MatchString(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`, hostname); !matched {
			return fmt.Errorf("invalid hostname %q", hostname)
		}
	}
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].PublicHostname = hostname
			return SaveProjects(projects)
		}
	}
	return fmt.Errorf("project not found: %s", name)
}

// SetProjectWebserver overrides a project's webserver choice. Empty = auto.
func SetProjectWebserver(name, ws string) error {
	if ws != "" && !validWebservers[ws] {
		return fmt.Errorf("invalid webserver %q", ws)
	}
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Name == name {
			projects[i].Webserver = ws
			return SaveProjects(projects)
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
