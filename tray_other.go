//go:build !windows && !darwin

package main

// No tray on other platforms (Linux is only used for CI vet/test runs).
func (a *App) setupTray() (start func(), end func()) {
	return func() {}, func() {}
}
