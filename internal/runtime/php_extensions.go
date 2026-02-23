package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// PHPExtension represents a PHP extension entry in php.ini
type PHPExtension struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ensureExtensionDir makes sure extension_dir is set to the correct absolute path in php.ini.
// PHP on Windows needs an absolute extension_dir pointing to the ext/ folder inside the PHP install directory,
// otherwise it tries the default C:\php\ext which doesn't exist for DevBox installs.
func ensureExtensionDir(phpDir string) error {
	iniPath := filepath.Join(phpDir, "php.ini")

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return err
	}

	extDir := filepath.Join(phpDir, "ext")
	// Use forward slashes for PHP compatibility
	extDirSlash := strings.ReplaceAll(extDir, "\\", "/")
	desiredLine := fmt.Sprintf(`extension_dir = "%s"`, extDirSlash)

	content := string(data)
	lines := strings.Split(content, "\n")

	// Check if extension_dir is already set correctly (uncommented, absolute path)
	extDirRe := regexp.MustCompile(`^\s*extension_dir\s*=`)
	commentedExtDirRe := regexp.MustCompile(`^\s*;\s*extension_dir\s*=`)

	foundActive := false
	for _, line := range lines {
		if extDirRe.MatchString(line) && !commentedExtDirRe.MatchString(line) {
			// There's an active extension_dir line - check if it points to the right place
			if strings.Contains(line, extDirSlash) || strings.Contains(line, strings.ReplaceAll(extDir, "/", "\\")) {
				return nil // already correct
			}
			foundActive = true
			break
		}
	}

	if foundActive {
		// Replace the active but wrong extension_dir
		for i, line := range lines {
			if extDirRe.MatchString(line) && !commentedExtDirRe.MatchString(line) {
				lines[i] = desiredLine
				break
			}
		}
	} else {
		// No active extension_dir found - find the last commented one and add after it
		lastCommentedIdx := -1
		for i, line := range lines {
			if commentedExtDirRe.MatchString(line) {
				lastCommentedIdx = i
			}
		}

		if lastCommentedIdx >= 0 {
			// Insert after the last commented extension_dir line
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:lastCommentedIdx+1]...)
			newLines = append(newLines, desiredLine)
			newLines = append(newLines, lines[lastCommentedIdx+1:]...)
			lines = newLines
		} else {
			// No extension_dir line at all - add near the top after [PHP] section
			inserted := false
			for i, line := range lines {
				if strings.TrimSpace(line) == "[PHP]" {
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:i+1]...)
					newLines = append(newLines, desiredLine)
					newLines = append(newLines, lines[i+1:]...)
					lines = newLines
					inserted = true
					break
				}
			}
			if !inserted {
				lines = append([]string{desiredLine}, lines...)
			}
		}
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}

// GetPHPExtensions returns the list of extensions from php.ini for a given PHP version.
// Only lists extensions whose DLL files actually exist in the ext/ directory.
func GetPHPExtensions(version string) ([]PHPExtension, error) {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	iniPath := filepath.Join(phpDir, "php.ini")

	// If php.ini doesn't exist, copy from php.ini-development
	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		devIni := filepath.Join(phpDir, "php.ini-development")
		if _, err := os.Stat(devIni); err == nil {
			data, err := os.ReadFile(devIni)
			if err != nil {
				return nil, err
			}
			os.WriteFile(iniPath, data, 0644)
		} else {
			return nil, fmt.Errorf("php.ini not found for PHP %s", version)
		}
	}

	// Ensure extension_dir is correctly set
	ensureExtensionDir(phpDir)

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return nil, err
	}

	// Build a set of DLLs that actually exist in ext/
	extDir := filepath.Join(phpDir, "ext")
	availableDLLs := map[string]bool{}
	if entries, err := os.ReadDir(extDir); err == nil {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if strings.HasSuffix(name, ".dll") {
				// php_pdo_pgsql.dll -> pdo_pgsql
				stripped := strings.TrimSuffix(name, ".dll")
				stripped = strings.TrimPrefix(stripped, "php_")
				availableDLLs[stripped] = true
			}
		}
	}

	seen := map[string]bool{}
	var extensions []PHPExtension

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Match: extension=xxx or ;extension=xxx
		enabled := false
		var extLine string

		if strings.HasPrefix(trimmed, "extension=") {
			enabled = true
			extLine = trimmed
		} else if strings.HasPrefix(trimmed, ";extension=") {
			enabled = false
			extLine = strings.TrimPrefix(trimmed, ";")
		} else {
			continue
		}

		// Extract extension name
		name := strings.TrimPrefix(extLine, "extension=")
		name = strings.TrimSpace(name)
		// Remove .dll suffix if present
		name = strings.TrimSuffix(name, ".dll")
		// Remove .so suffix if present
		name = strings.TrimSuffix(name, ".so")

		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		// Only show extensions whose DLL actually exists
		if !availableDLLs[name] {
			continue
		}

		extensions = append(extensions, PHPExtension{
			Name:    name,
			Enabled: enabled,
		})
	}

	sort.Slice(extensions, func(i, j int) bool {
		return extensions[i].Name < extensions[j].Name
	})

	return extensions, nil
}

// commonExtensions are extensions needed by most PHP frameworks (Laravel, WordPress, Symfony etc.)
var commonExtensions = []string{
	"mbstring",
	"openssl",
	"pdo_mysql",
	"pdo_pgsql",
	"pdo_sqlite",
	"fileinfo",
	"curl",
	"tokenizer",
	"xml",
	"ctype",
	"bcmath",
	"gd",
	"zip",
	"intl",
	"sodium",
	"exif",
	"gettext",
}

// EnableCommonExtensions enables common PHP extensions needed by frameworks.
// Only enables extensions whose DLL files actually exist in the ext/ directory.
func EnableCommonExtensions(version string) {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	extDir := filepath.Join(phpDir, "ext")

	for _, ext := range commonExtensions {
		// Check if the DLL exists
		dllPath := filepath.Join(extDir, "php_"+ext+".dll")
		if _, err := os.Stat(dllPath); os.IsNotExist(err) {
			continue
		}
		// Enable silently - ignore errors for individual extensions
		TogglePHPExtension(version, ext, true)
	}
}

// TogglePHPExtension enables or disables a PHP extension in php.ini
func TogglePHPExtension(version, extName string, enable bool) error {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	iniPath := filepath.Join(phpDir, "php.ini")

	// Ensure extension_dir is set before toggling
	if err := ensureExtensionDir(phpDir); err != nil {
		return fmt.Errorf("cannot set extension_dir: %w", err)
	}

	// Verify the DLL actually exists before enabling
	if enable {
		extDir := filepath.Join(phpDir, "ext")
		dllName := "php_" + extName + ".dll"
		dllPath := filepath.Join(extDir, dllName)
		if _, err := os.Stat(dllPath); os.IsNotExist(err) {
			return fmt.Errorf("extension DLL not found: %s", dllPath)
		}
	}

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return fmt.Errorf("cannot read php.ini: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Match both enabled and disabled forms
		var isMatch bool
		if strings.HasPrefix(trimmed, "extension="+extName) ||
			strings.HasPrefix(trimmed, "extension="+extName+".dll") ||
			strings.HasPrefix(trimmed, "extension="+extName+".so") {
			isMatch = true
		} else if strings.HasPrefix(trimmed, ";extension="+extName) ||
			strings.HasPrefix(trimmed, ";extension="+extName+".dll") ||
			strings.HasPrefix(trimmed, ";extension="+extName+".so") {
			isMatch = true
		}

		if !isMatch {
			continue
		}

		found = true
		// Get the original extension value (with or without .dll)
		original := trimmed
		if strings.HasPrefix(original, ";") {
			original = strings.TrimPrefix(original, ";")
		}

		if enable {
			lines[i] = original
		} else {
			lines[i] = ";" + original
		}
		break
	}

	if !found {
		// Add new extension entry
		if enable {
			lines = append(lines, fmt.Sprintf("extension=%s", extName))
		}
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}
