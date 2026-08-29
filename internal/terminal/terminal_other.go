//go:build !windows && !darwin

package terminal

import "fmt"

// Linux is not a supported DevBox target yet; keep the package compiling for
// CI (go vet on ubuntu) and cross-checks.
func detect() []Terminal { return nil }

func launch(t Terminal, s Session, script string) error {
	return fmt.Errorf("terminal integration is not available on this platform")
}
