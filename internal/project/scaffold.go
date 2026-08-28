package project

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"

	"DevBox/internal/platform"
	"DevBox/internal/runtime"
)

// FrameworkTemplate represents a project template that can be scaffolded
type FrameworkTemplate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Category        string `json:"category"`        // "php", "node", "go", "rust", "python", "static"
	RequiredRuntime string `json:"requiredRuntime"` // runtime.Registry key
	RequiresTool    string `json:"requiresTool"`    // "composer" or ""
	Available       bool   `json:"available"`       // computed at runtime
	RuntimeVersion  string `json:"runtimeVersion"`  // computed: active version
}

// ScaffoldProgress represents scaffold/clone progress
type ScaffoldProgress struct {
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

var scaffoldMu sync.Mutex

var templates = []FrameworkTemplate{
	{ID: "laravel", Name: "Laravel", Category: "php", RequiredRuntime: "php", RequiresTool: "composer"},
	{ID: "symfony", Name: "Symfony", Category: "php", RequiredRuntime: "php", RequiresTool: "composer"},
	{ID: "wordpress", Name: "WordPress", Category: "php", RequiredRuntime: "php", RequiresTool: "composer"},
	{ID: "php", Name: "PHP", Category: "php", RequiredRuntime: "php", RequiresTool: "composer"},
	{ID: "nextjs", Name: "Next.js", Category: "node", RequiredRuntime: "node"},
	{ID: "nuxt", Name: "Nuxt", Category: "node", RequiredRuntime: "node"},
	{ID: "vue", Name: "Vue", Category: "node", RequiredRuntime: "node"},
	{ID: "react", Name: "React", Category: "node", RequiredRuntime: "node"},
	{ID: "svelte", Name: "Svelte", Category: "node", RequiredRuntime: "node"},
	{ID: "angular", Name: "Angular", Category: "node", RequiredRuntime: "node"},
	{ID: "go", Name: "Go", Category: "go", RequiredRuntime: "go"},
	{ID: "rust", Name: "Rust", Category: "rust", RequiredRuntime: "rust"},
	{ID: "django", Name: "Django", Category: "python", RequiredRuntime: "python"},
	{ID: "static", Name: "Static HTML", Category: "static"},
}

// GetTemplates returns all available templates with computed availability
func GetTemplates() []FrameworkTemplate {
	result := make([]FrameworkTemplate, len(templates))
	copy(result, templates)

	for i := range result {
		tmpl := &result[i]
		if tmpl.RequiredRuntime == "" {
			// static — always available
			tmpl.Available = true
			continue
		}

		mgr, ok := runtime.Registry[tmpl.RequiredRuntime]
		if !ok {
			tmpl.Available = false
			continue
		}

		ver, _ := mgr.GetGlobal()
		if ver == "" {
			tmpl.Available = false
			continue
		}
		tmpl.RuntimeVersion = ver

		// Check tool requirement
		if tmpl.RequiresTool == "composer" {
			tmpl.Available = runtime.IsComposerInstalled()
		} else {
			tmpl.Available = true
		}
	}

	return result
}

// ScaffoldProject creates a new project from a template
func ScaffoldProject(templateID, parentDir, projectName string, progress chan<- ScaffoldProgress) error {
	if !scaffoldMu.TryLock() {
		return fmt.Errorf("another scaffold operation is already running")
	}
	defer scaffoldMu.Unlock()

	// Validate inputs
	if projectName == "" {
		return fmt.Errorf("project name is required")
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, projectName); !matched {
		return fmt.Errorf("project name can only contain letters, numbers, hyphens and underscores")
	}

	projectPath := filepath.Join(parentDir, projectName)
	if _, err := os.Stat(projectPath); err == nil {
		return fmt.Errorf("directory already exists: %s", projectPath)
	}

	// Find template
	var tmpl *FrameworkTemplate
	for _, t := range GetTemplates() {
		if t.ID == templateID {
			tmpl = &t
			break
		}
	}
	if tmpl == nil {
		return fmt.Errorf("unknown template: %s", templateID)
	}

	if tmpl.RequiredRuntime != "" && !tmpl.Available {
		return fmt.Errorf("required runtime or tool is not available")
	}

	progress <- ScaffoldProgress{Percent: -1, Message: fmt.Sprintf("Creating %s project...", tmpl.Name)}

	var err error
	switch templateID {
	case "go":
		err = scaffoldGo(parentDir, projectName, progress)
	case "static":
		err = scaffoldStatic(parentDir, projectName, progress)
	case "php":
		err = scaffoldPHPComposer(parentDir, projectName, progress)
	default:
		err = scaffoldWithCommand(tmpl, parentDir, projectName, progress)
	}

	if err != nil {
		// Cleanup on failure
		os.RemoveAll(projectPath)
		return err
	}

	progress <- ScaffoldProgress{Percent: 100, Message: "Done!"}
	return nil
}

func scaffoldGo(parentDir, name string, progress chan<- ScaffoldProgress) error {
	projectPath := filepath.Join(parentDir, name)
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return err
	}

	mgr := runtime.Registry["go"]
	ver, _ := mgr.GetGlobal()
	binDir := mgr.BinaryPath(ver)
	goExe := filepath.Join(binDir, platform.BinaryName("go"))

	progress <- ScaffoldProgress{Percent: -1, Message: "Running go mod init..."}

	cmd := exec.Command(goExe, "mod", "init", name)
	cmd.Dir = projectPath
	cmd.Env = buildEnv(binDir)
	platform.SetProcessAttrs(cmd, true, true)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod init failed: %s\n%s", err, string(out))
	}

	progress <- ScaffoldProgress{Percent: -1, Message: "Creating main.go..."}

	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	if err := os.WriteFile(filepath.Join(projectPath, "main.go"), []byte(mainGo), 0644); err != nil {
		return err
	}

	return nil
}

func scaffoldStatic(parentDir, name string, progress chan<- ScaffoldProgress) error {
	projectPath := filepath.Join(parentDir, name)
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return err
	}

	progress <- ScaffoldProgress{Percent: -1, Message: "Creating index.html..."}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
</head>
<body>
    <h1>Welcome to %s</h1>
</body>
</html>
`, name, name)

	return os.WriteFile(filepath.Join(projectPath, "index.html"), []byte(html), 0644)
}

func scaffoldPHPComposer(parentDir, name string, progress chan<- ScaffoldProgress) error {
	projectPath := filepath.Join(parentDir, name)
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return err
	}

	mgr := runtime.Registry["php"]
	ver, _ := mgr.GetGlobal()
	binDir := mgr.BinaryPath(ver)
	phpExe := filepath.Join(binDir, platform.BinaryName("php"))
	composerPhar := filepath.Join(binDir, "composer.phar")

	progress <- ScaffoldProgress{Percent: -1, Message: "Running composer init..."}

	cmd := exec.Command(phpExe, composerPhar, "init",
		"--name=devbox/"+name,
		"--no-interaction",
	)
	cmd.Dir = projectPath
	cmd.Env = buildEnv(binDir)
	platform.SetProcessAttrs(cmd, true, true)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("composer init failed: %s\n%s", err, string(out))
	}

	// Create a basic index.php
	indexPHP := `<?php
echo "Hello from " . basename(__DIR__) . "!";
`
	os.WriteFile(filepath.Join(projectPath, "index.php"), []byte(indexPHP), 0644)

	return nil
}

func scaffoldWithCommand(tmpl *FrameworkTemplate, parentDir, projectName string, progress chan<- ScaffoldProgress) error {
	mgr := runtime.Registry[tmpl.RequiredRuntime]
	ver, _ := mgr.GetGlobal()
	binDir := mgr.BinaryPath(ver)

	var cmdName string
	var args []string

	switch tmpl.ID {
	case "laravel":
		phpExe := filepath.Join(binDir, platform.BinaryName("php"))
		composerPhar := filepath.Join(binDir, "composer.phar")
		cmdName = phpExe
		args = []string{composerPhar, "create-project", "laravel/laravel", projectName}
	case "symfony":
		phpExe := filepath.Join(binDir, platform.BinaryName("php"))
		composerPhar := filepath.Join(binDir, "composer.phar")
		cmdName = phpExe
		args = []string{composerPhar, "create-project", "symfony/skeleton", projectName}
	case "wordpress":
		phpExe := filepath.Join(binDir, platform.BinaryName("php"))
		composerPhar := filepath.Join(binDir, "composer.phar")
		cmdName = phpExe
		args = []string{composerPhar, "create-project", "johnpbloch/wordpress", projectName}
	case "nextjs":
		cmdName = filepath.Join(binDir, platform.ScriptName("npx"))
		args = []string{"create-next-app@latest", projectName, "--use-npm"}
	case "nuxt":
		cmdName = filepath.Join(binDir, platform.ScriptName("npx"))
		args = []string{"nuxi@latest", "init", projectName}
	case "vue":
		cmdName = filepath.Join(binDir, platform.ScriptName("npm"))
		args = []string{"create", "vite@latest", projectName, "--", "--template", "vue"}
	case "react":
		cmdName = filepath.Join(binDir, platform.ScriptName("npm"))
		args = []string{"create", "vite@latest", projectName, "--", "--template", "react"}
	case "svelte":
		cmdName = filepath.Join(binDir, platform.ScriptName("npm"))
		args = []string{"create", "vite@latest", projectName, "--", "--template", "svelte"}
	case "angular":
		cmdName = filepath.Join(binDir, platform.ScriptName("npx"))
		args = []string{"@angular/cli", "new", projectName, "--skip-git", "--defaults"}
	case "rust":
		cmdName = filepath.Join(binDir, platform.BinaryName("cargo"))
		args = []string{"init", projectName}
	case "django":
		cmdName = filepath.Join(binDir, platform.BinaryName("python"))
		args = []string{"-m", "django", "startproject", projectName}
	default:
		return fmt.Errorf("unsupported template: %s", tmpl.ID)
	}

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = parentDir
	cmd.Env = buildEnv(binDir)
	platform.SetProcessAttrs(cmd, true, true)

	// Combine stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start: %s", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			progress <- ScaffoldProgress{Percent: -1, Message: line}
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command failed: %s", err)
	}

	return nil
}

// CloneProject clones a git repository
func CloneProject(gitURL, parentDir, projectName string, progress chan<- ScaffoldProgress) error {
	if !scaffoldMu.TryLock() {
		return fmt.Errorf("another scaffold operation is already running")
	}
	defer scaffoldMu.Unlock()

	gitExe := findGitExe()
	if gitExe == "" {
		return fmt.Errorf("git is not installed or not found in PATH")
	}

	// Derive project name from URL if empty
	if projectName == "" {
		parts := strings.Split(strings.TrimSuffix(gitURL, "/"), "/")
		projectName = strings.TrimSuffix(parts[len(parts)-1], ".git")
	}

	projectPath := filepath.Join(parentDir, projectName)
	if _, err := os.Stat(projectPath); err == nil {
		return fmt.Errorf("directory already exists: %s", projectPath)
	}

	progress <- ScaffoldProgress{Percent: -1, Message: "Cloning repository..."}

	cmd := exec.Command(gitExe, "clone", "--progress", gitURL, projectName)
	cmd.Dir = parentDir
	platform.SetProcessAttrs(cmd, true, true)

	// git clone progress goes to stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git clone failed to start: %s", err)
	}

	// Read stderr line by line for progress
	go func() {
		reader := bufio.NewReader(stderr)
		for {
			line, err := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line != "" {
				// Try to parse git progress (e.g. "Receiving objects:  50% (100/200)")
				percent := parseGitProgress(line)
				progress <- ScaffoldProgress{Percent: percent, Message: line}
			}
			if err != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		os.RemoveAll(projectPath)
		return fmt.Errorf("git clone failed: %s", err)
	}

	progress <- ScaffoldProgress{Percent: 100, Message: "Clone complete!"}
	return nil
}

// parseGitProgress tries to extract percentage from git progress output
func parseGitProgress(line string) int {
	// Matches patterns like "Receiving objects:  45% (123/456)"
	re := regexp.MustCompile(`(\d+)%`)
	matches := re.FindStringSubmatch(line)
	if len(matches) >= 2 {
		var pct int
		fmt.Sscanf(matches[1], "%d", &pct)
		return pct
	}
	return -1
}

// IsGitInstalled returns true if git is available
func IsGitInstalled() bool {
	return findGitExe() != ""
}

// findGitExe locates the git executable
func findGitExe() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	// Platform-specific common paths
	if goruntime.GOOS == "windows" {
		commonPaths := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "cmd", "git.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "cmd", "git.exe"),
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	} else {
		commonPaths := []string{
			"/usr/bin/git",
			"/usr/local/bin/git",
			"/opt/homebrew/bin/git",
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// buildEnv creates a modified environment with binDir prepended to PATH
func buildEnv(binDir string) []string {
	env := os.Environ()
	result := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			result = append(result, "PATH="+binDir+string(os.PathListSeparator)+e[5:])
		} else {
			result = append(result, e)
		}
	}
	return result
}

