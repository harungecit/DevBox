//go:build darwin || linux

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- User environment variables (<data>/env.sh, sourced from path.sh) ---

func envShFile() string {
	return filepath.Join((&darwinPlatform{}).DefaultDataDir(), "env.sh")
}

func readEnvSh() (map[string]string, error) {
	vars := map[string]string{}
	data, err := os.ReadFile(envShFile())
	if err != nil {
		if os.IsNotExist(err) {
			return vars, nil
		}
		return nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// export KEY="value"
		if !strings.HasPrefix(line, "export ") {
			continue
		}
		kv := strings.TrimPrefix(line, "export ")
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			continue
		}
		key := kv[:eq]
		val := strings.TrimSuffix(strings.TrimPrefix(kv[eq+1:], `"`), `"`)
		vars[key] = strings.ReplaceAll(val, `\"`, `"`)
	}
	return vars, nil
}

func writeEnvSh(vars map[string]string) error {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{"# DevBox managed environment variables — do not edit manually"}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf(`export %s="%s"`, k, strings.ReplaceAll(vars[k], `"`, `\"`)))
	}
	file := envShFile()
	os.MkdirAll(filepath.Dir(file), 0755)
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ensureEnvSource makes path.sh source env.sh so a single ~/.zshrc line
// (the one ensureShellSource adds) covers both files.
func (d *darwinPlatform) ensureEnvSource() error {
	pathFile := pathShFile()
	sourceLine := fmt.Sprintf(`[ -f "%s" ] && source "%s"`, envShFile(), envShFile())
	data, err := os.ReadFile(pathFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !strings.Contains(string(data), sourceLine) {
		content := string(data)
		if content == "" {
			content = "# DevBox managed PATH entries — do not edit manually\n"
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "# DevBox managed environment variables\n" + sourceLine + "\n"
		os.MkdirAll(filepath.Dir(pathFile), 0755)
		if err := os.WriteFile(pathFile, []byte(content), 0644); err != nil {
			return err
		}
	}
	return d.ensureShellSource()
}

func (d *darwinPlatform) SetUserEnv(key, value string) error {
	if strings.EqualFold(key, "PATH") {
		return errors.New("PATH must be changed through the PATH functions")
	}
	vars, err := readEnvSh()
	if err != nil {
		return err
	}
	vars[key] = value
	if err := writeEnvSh(vars); err != nil {
		return err
	}
	return d.ensureEnvSource()
}

func (d *darwinPlatform) UnsetUserEnv(key string) error {
	vars, err := readEnvSh()
	if err != nil {
		return err
	}
	delete(vars, key)
	return writeEnvSh(vars)
}

func (d *darwinPlatform) GetUserEnv(key string) (string, bool) {
	vars, err := readEnvSh()
	if err != nil {
		return "", false
	}
	v, ok := vars[key]
	return v, ok
}
