//go:build windows || (darwin && cgo)

package main

import (
	"DevBox/internal/i18n"
	"DevBox/internal/service"

	"github.com/energye/systray"
)

// buildTrayMenu adds the static tray menu. Labels come from the active locale
// at build time; the menu is rebuilt on language change via rebuildTrayMenu.
func (a *App) buildTrayMenu() {
	t := func(key string) string { return i18n.T(key) }

	open := systray.AddMenuItem(t("tray.open"), "")
	open.Click(func() { a.showWindow() })

	systray.AddSeparator()

	startAll := systray.AddMenuItem(t("tray.startAll"), "")
	startAll.Click(func() {
		go func() {
			for name, mgr := range service.Registry {
				if mgr.IsInstalled() && mgr.Status() != service.StatusRunning {
					if err := mgr.Start(); err != nil {
						debugLog("tray start %s: %v", name, err)
					}
				}
			}
			a.emitServicesChanged()
		}()
	})

	stopAll := systray.AddMenuItem(t("tray.stopAll"), "")
	stopAll.Click(func() {
		go func() {
			service.StopAll()
			a.emitServicesChanged()
		}()
	})

	systray.AddSeparator()

	quit := systray.AddMenuItem(t("tray.quit"), "")
	quit.Click(func() { a.quit() })
}

// rebuildTrayMenu re-creates the menu (used after a language change).
func (a *App) rebuildTrayMenu() {
	systray.ResetMenu()
	a.buildTrayMenu()
}
