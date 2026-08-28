//go:build darwin && !cgo

package main

// The macOS tray needs cgo (AppKit). Cross-compiling from Windows with
// CGO_ENABLED=0 is only used as a compile check, so a no-op keeps that build green.
func (a *App) setupTray() (start func(), end func()) {
	return func() {}, func() {}
}
