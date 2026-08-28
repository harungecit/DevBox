package runtime

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"DevBox/internal/platform"
)

// allowedIniDirectives is the whitelist of php.ini directives that can be edited via the UI.
var allowedIniDirectives = map[string]bool{
	"max_execution_time":  true,
	"memory_limit":        true,
	"upload_max_filesize": true,
	"post_max_size":       true,
	"max_file_uploads":    true,
	"display_errors":      true,
	"error_reporting":     true,
	"date.timezone":       true,
}

// defaultIniValues are PHP default values for common directives.
var defaultIniValues = map[string]string{
	"max_execution_time":  "30",
	"memory_limit":        "128M",
	"upload_max_filesize": "2M",
	"post_max_size":       "8M",
	"max_file_uploads":    "20",
	"display_errors":      "Off",
	"error_reporting":     "E_ALL & ~E_DEPRECATED & ~E_STRICT",
	"date.timezone":       "UTC",
}

// GetPHPIniPath returns the php.ini file path for a given PHP version.
func GetPHPIniPath(version string) string {
	return filepath.Join(runtimeBaseDir("php"), version, "php.ini")
}

// GetPHPIniSettings reads common php.ini directives and returns their values.
func GetPHPIniSettings(version string) (map[string]string, error) {
	iniPath := GetPHPIniPath(version)

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read php.ini: %w", err)
	}

	// Start with defaults
	result := make(map[string]string)
	for k, v := range defaultIniValues {
		result[k] = v
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}

		for directive := range allowedIniDirectives {
			re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(directive) + `\s*=\s*(.*)`)
			if m := re.FindStringSubmatch(trimmed); m != nil {
				result[directive] = strings.TrimSpace(m[1])
			}
		}
	}

	return result, nil
}

// SetPHPIniSetting updates a single directive in php.ini.
// If the directive is commented out, it uncomments it. If it doesn't exist, it adds it.
func SetPHPIniSetting(version, key, value string) error {
	if !allowedIniDirectives[key] {
		return fmt.Errorf("directive not allowed: %s", key)
	}

	iniPath := GetPHPIniPath(version)

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return fmt.Errorf("cannot read php.ini: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	activeRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	commentedRe := regexp.MustCompile(`^\s*;\s*` + regexp.QuoteMeta(key) + `\s*=`)
	newLine := key + " = " + value

	found := false
	for i, line := range lines {
		if activeRe.MatchString(line) && !commentedRe.MatchString(line) {
			lines[i] = newLine
			found = true
			break
		}
	}

	if !found {
		// Look for a commented version and insert after it
		lastCommentedIdx := -1
		for i, line := range lines {
			if commentedRe.MatchString(line) {
				lastCommentedIdx = i
			}
		}

		if lastCommentedIdx >= 0 {
			// Insert active line after the last commented occurrence
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:lastCommentedIdx+1]...)
			newLines = append(newLines, newLine)
			newLines = append(newLines, lines[lastCommentedIdx+1:]...)
			lines = newLines
		} else {
			// No existing line at all — append at the end
			lines = append(lines, newLine)
		}
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}

// PHPExtension represents a PHP extension entry in php.ini
type PHPExtension struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Zend is true for engine extensions loaded via `zend_extension=` (opcache, xdebug).
	Zend bool `json:"zend"`
	// Source is "bundled" (shipped with the PHP build) or "pecl" (installed by DevBox).
	Source string `json:"source"`
}

// zendExtensions must be loaded with `zend_extension=`; `extension=` silently
// fails for them ("must be loaded as a Zend extension").
var zendExtensions = map[string]bool{"opcache": true, "xdebug": true}

func extDirective(name string) string {
	if zendExtensions[name] {
		return "zend_extension="
	}
	return "extension="
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

// extFileName returns the platform-specific extension filename.
// Windows: "php_curl.dll", macOS: "curl.so"
func extFileName(extName string) string {
	if platform.LibExt() == ".dll" {
		return "php_" + extName + ".dll"
	}
	return extName + platform.LibExt()
}

// extNameFromFile extracts the extension name from a filename.
// Windows: "php_curl.dll" → "curl", macOS: "curl.so" → "curl"
func extNameFromFile(filename string) string {
	name := strings.TrimSuffix(filename, platform.LibExt())
	if platform.LibExt() == ".dll" {
		name = strings.TrimPrefix(name, "php_")
	}
	return name
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
			if strings.HasSuffix(name, platform.LibExt()) {
				stripped := extNameFromFile(name)
				availableDLLs[stripped] = true
			}
		}
	}

	seen := map[string]bool{}
	var extensions []PHPExtension
	peclInstalled := installedPeclSet(phpDir)

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Match: extension=xxx / zend_extension=xxx, commented or not.
		enabled := !strings.HasPrefix(trimmed, ";")
		extLine := strings.TrimLeft(trimmed, "; \t")
		if strings.HasPrefix(extLine, "zend_extension=") {
			extLine = strings.TrimPrefix(extLine, "zend_extension=")
		} else if strings.HasPrefix(extLine, "extension=") {
			extLine = strings.TrimPrefix(extLine, "extension=")
		} else {
			continue
		}

		// Extract extension name: strip quotes, path, prefix and suffix.
		name := strings.Trim(strings.TrimSpace(extLine), `"`)
		name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
		name = strings.ToLower(extNameFromFile(strings.TrimSuffix(strings.TrimSuffix(name, ".dll"), ".so")))
		if platform.LibExt() == ".dll" {
			name = strings.TrimPrefix(name, "php_")
		}

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
			Zend:    zendExtensions[name],
			Source:  sourceFor(name, peclInstalled),
		})
	}

	// Extensions shipped in ext/ that php.ini never mentions (test modules,
	// PECL DLLs dropped in by hand) are still toggleable — list them disabled.
	for name := range availableDLLs {
		if seen[name] || strings.HasSuffix(name, "_test") {
			continue
		}
		extensions = append(extensions, PHPExtension{Name: name, Zend: zendExtensions[name], Source: sourceFor(name, peclInstalled)})
	}

	sort.Slice(extensions, func(i, j int) bool {
		return extensions[i].Name < extensions[j].Name
	})

	return extensions, nil
}

func sourceFor(name string, pecl map[string]bool) string {
	if pecl[name] {
		return "pecl"
	}
	return "bundled"
}

// commonExtensions are enabled at install time so a fresh PHP works for Laravel, WordPress,
// Symfony and similar frameworks without manual php.ini editing. Built-ins (ctype, dom, json,
// pdo, phar, session, simplexml, spl, standard, tokenizer, xml*) are listed for explicitness;
// EnableCommonExtensions skips any whose DLL/.so is not actually shipped with that PHP build.
var commonExtensions = []string{
	"bz2",
	"calendar",
	"curl",
	"exif",
	"fileinfo",
	"gd",
	"gettext",
	"gmp",
	"intl",
	"ldap",
	"mbstring",
	"mysqli",
	"opcache",
	"openssl",
	"pdo_mysql",
	"pdo_pgsql",
	"pdo_sqlite",
	"pgsql",
	"soap",
	"sockets",
	"sodium",
	"sqlite3",
	"tidy",
	"xsl",
	"zip",
}

// devIniDirectives are the development-friendly php.ini values applied at install time.
// Tuned for typical local web/framework development — generous limits, full error visibility,
// opcache on with timestamp validation so file changes are picked up immediately.
var devIniDirectives = map[string]string{
	// Resource limits — generous so large requests / long-running scripts don't fail in dev.
	"memory_limit":        "512M",
	"max_execution_time":  "600",
	"max_input_time":      "600",
	"post_max_size":       "100M",
	"upload_max_filesize": "100M",
	"max_file_uploads":    "50",
	"max_input_vars":      "10000",

	// Locale / charset / hardening for dev.
	"default_charset":     "UTF-8",
	"date.timezone":       "UTC",
	"expose_php":          "Off",
	"realpath_cache_size": "4096K",
	"realpath_cache_ttl":  "600",

	// Errors — full visibility for dev.
	"display_errors":         "On",
	"display_startup_errors": "On",
	"log_errors":             "On",
	"error_reporting":        "E_ALL",

	// Opcache — fast page loads but pick up edits immediately (validate_timestamps + revalidate_freq=0).
	"opcache.enable":                  "1",
	"opcache.enable_cli":              "0",
	"opcache.memory_consumption":      "256",
	"opcache.interned_strings_buffer": "16",
	"opcache.max_accelerated_files":   "20000",
	"opcache.validate_timestamps":     "1",
	"opcache.revalidate_freq":         "0",
	"opcache.fast_shutdown":           "1",
}

// ApplyDevPreset configures a freshly installed PHP for development use:
// ensures php.ini exists (from php.ini-development), sets extension_dir, applies
// devIniDirectives, and enables commonExtensions. Idempotent — safe to call repeatedly.
func ApplyDevPreset(version string) error {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	iniPath := filepath.Join(phpDir, "php.ini")

	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		src := filepath.Join(phpDir, "php.ini-development")
		if _, err := os.Stat(src); os.IsNotExist(err) {
			src = filepath.Join(phpDir, "php.ini-production")
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("no php.ini template found: %w", err)
		}
		if err := os.WriteFile(iniPath, data, 0644); err != nil {
			return fmt.Errorf("cannot write php.ini: %w", err)
		}
	}

	if err := ensureExtensionDir(phpDir); err != nil {
		return fmt.Errorf("cannot set extension_dir: %w", err)
	}

	if err := applyIniDirectives(iniPath, devIniDirectives); err != nil {
		return fmt.Errorf("cannot apply dev ini directives: %w", err)
	}

	EnableCommonExtensions(version)
	return nil
}

// applyIniDirectives sets a batch of directives in an ini file. For each key it either
// rewrites the existing active line, uncomments + sets the last commented form, or
// appends a new entry at the end.
func applyIniDirectives(iniPath string, directives map[string]string) error {
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	keys := make([]string, 0, len(directives))
	for k := range directives {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := directives[key]
		activeRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
		commentedRe := regexp.MustCompile(`^\s*;\s*` + regexp.QuoteMeta(key) + `\s*=`)
		newLine := key + " = " + value
		replaced := false

		for i, line := range lines {
			if activeRe.MatchString(line) && !commentedRe.MatchString(line) {
				lines[i] = newLine
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}

		lastCommentedIdx := -1
		for i, line := range lines {
			if commentedRe.MatchString(line) {
				lastCommentedIdx = i
			}
		}
		if lastCommentedIdx >= 0 {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:lastCommentedIdx+1]...)
			newLines = append(newLines, newLine)
			newLines = append(newLines, lines[lastCommentedIdx+1:]...)
			lines = newLines
		} else {
			lines = append(lines, newLine)
		}
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}

// EnableCommonExtensions enables common PHP extensions needed by frameworks.
// Only enables extensions whose DLL files actually exist in the ext/ directory.
func EnableCommonExtensions(version string) {
	phpDir := filepath.Join(runtimeBaseDir("php"), version)
	extDir := filepath.Join(phpDir, "ext")

	for _, ext := range commonExtensions {
		// Check if the extension file exists
		dllPath := filepath.Join(extDir, extFileName(ext))
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

	// Verify the extension file actually exists before enabling
	if enable {
		extDir := filepath.Join(phpDir, "ext")
		dllName := extFileName(extName)
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
	// Matches `extension=NAME`, `zend_extension=NAME`, with optional `;`,
	// optional php_ prefix / .dll / .so suffix, exactly this extension.
	re := regexp.MustCompile(`^\s*;?\s*(zend_)?extension\s*=\s*"?(php_)?` + regexp.QuoteMeta(extName) + `(\.dll|\.so)?"?\s*$`)

	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		found = true
		original := strings.TrimLeft(strings.TrimSpace(line), "; \t")
		// Correct the directive kind in case an older DevBox wrote `extension=opcache`.
		if zendExtensions[extName] && !strings.HasPrefix(original, "zend_") {
			original = "zend_" + original
		}
		if enable {
			lines[i] = original
		} else {
			lines[i] = ";" + original
		}
		// Don't break: duplicates (e.g. an appended line) are normalised too.
	}

	if !found && enable {
		lines = append(lines, extDirective(extName)+extName)
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}
