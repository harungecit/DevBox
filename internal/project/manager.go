package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"DevBox/internal/config"
	"DevBox/internal/platform"
	"DevBox/internal/runtime"
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
	// AutoStart keeps an app-server project's dev server up: started with
	// DevBox and restarted (with backoff) if it exits unexpectedly.
	AutoStart bool `json:"autoStart,omitempty"`

	// Runtime is the language/category the project is built on. Independent of
	// Framework so it survives framework misdetection. Derived from Framework if
	// empty during ListProjects (backward-compat migration for older entries).
	// Values: any registered runtime name ("php", "node", "go", "python",
	// "rust", or a plugin runtime such as "java") | "static".
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

	// HostsRegistered is computed on read (not persisted): whether Domain is
	// currently mapped in the OS hosts file, i.e. actually reachable.
	HostsRegistered bool `json:"hostsRegistered"`
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
	hosts := hostsFileDomains()
	for i := range projects {
		projects[i].fillDefaults()
		projects[i].HostsRegistered = hosts[strings.ToLower(projects[i].Domain)]
	}
	return projects, nil
}

// hostsFileDomains reads the hosts file once and returns every mapped name.
func hostsFileDomains() map[string]bool {
	out := map[string]bool{}
	data, err := platform.ReadHostsFile()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		for _, name := range f[min(1, len(f)):] {
			out[strings.ToLower(name)] = true
		}
	}
	return out
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
		domain = name
	}
	domain = NormalizeDomain(domain)

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
	// A legacy version file (.nvmrc, .java-version, .tool-versions...) pins
	// the project when its runtime is plugin-backed and knows the format.
	if v, ok := LegacyRuntimeVersion(rt, projectPath); ok {
		project.RuntimeVersion = v
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

// isValidRuntime / validWebservers gate the public setters so the persisted
// values stay within the vocabulary the rest of DevBox expects: any registered
// runtime (built-in or plugin) plus "static".
func isValidRuntime(rt string) bool {
	if rt == "static" {
		return true
	}
	_, ok := runtime.Registry[rt]
	return ok
}

var validWebservers = map[string]bool{
	"auto": true, "nginx": true, "caddy": true, "apache": true, "frankenphp": true, "devserver": true,
}

// SetProjectRuntime overrides a project's runtime. Empty string resets to
// auto-derived-from-Framework behavior.
func SetProjectRuntime(name, rt string) error {
	if rt != "" && !isValidRuntime(rt) {
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

// DocumentRoot returns the directory a web server should serve for a project:
// the first conventional front-controller folder that exists (Laravel/Symfony/
// CI4 public, CakePHP webroot, Yii/Drupal web, htdocs), else the project root.
func DocumentRoot(projectPath string) string {
	for _, sub := range []string{"public", "webroot", "web", "htdocs", "public_html"} {
		dir := filepath.Join(projectPath, sub)
		if fileExists(filepath.Join(dir, "index.php")) || fileExists(filepath.Join(dir, "index.html")) {
			return dir
		}
	}
	return projectPath
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// NormalizeDomain turns a free-form name ("Personal Project.Manager") into a
// valid local host name ("personal-project-manager.test"). A ".test"/".local"
// suffix is preserved; any other suffix is treated as part of the label.
func NormalizeDomain(in string) string {
	d := strings.ToLower(strings.TrimSpace(in))
	suffix := ".test"
	for _, s := range []string{".test", ".local"} {
		if strings.HasSuffix(d, s) {
			suffix = s
			d = strings.TrimSuffix(d, s)
			break
		}
	}
	var b strings.Builder
	lastDash := true // suppress leading dashes
	for _, r := range d {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	label := strings.Trim(b.String(), "-")
	if label == "" {
		label = "project"
	}
	return label + suffix
}
