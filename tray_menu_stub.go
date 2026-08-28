//go:build !windows && !(darwin && cgo)

package main

func (a *App) rebuildTrayMenu() {}
