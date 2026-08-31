//go:build darwin

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// LinkDir creates a symlink at `link` pointing at the directory `target`.
// Removing the link never touches the target.
func (m *darwinPlatform) LinkDir(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	fi, err := os.Stat(absTarget)
	if err != nil {
		return fmt.Errorf("link target not found: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("link target is not a directory: %s", absTarget)
	}
	return os.Symlink(absTarget, link)
}
