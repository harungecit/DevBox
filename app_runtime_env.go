package main

import (
	"DevBox/internal/runtime"
)

// Runtime activation helpers shared by install / update / uninstall / set-global.
//
// A built-in runtime contributes one bin directory to the user PATH; a plugin
// runtime may contribute several plus environment variables (JAVA_HOME,
// GOROOT…). runtime.ApplyGlobalEnv / ClearGlobalEnv know the difference, so
// the IPC layer never has to.

// activateRuntimeEnv switches the user environment from oldVersion to
// newVersion of a runtime. Either may be empty.
func (a *App) activateRuntimeEnv(mgr runtime.RuntimeManager, oldVersion, newVersion string) error {
	if oldVersion != "" && oldVersion != newVersion {
		if err := runtime.ClearGlobalEnv(mgr, oldVersion); err != nil {
			debugLog("clear env %s %s: %v", mgr.Name(), oldVersion, err)
		}
	}
	if newVersion == "" {
		return nil
	}
	return runtime.ApplyGlobalEnv(mgr, newVersion)
}

// deactivateRuntimeEnv removes a version's PATH entries and variables.
func (a *App) deactivateRuntimeEnv(mgr runtime.RuntimeManager, version string) {
	if version == "" {
		return
	}
	if err := runtime.ClearGlobalEnv(mgr, version); err != nil {
		debugLog("clear env %s %s: %v", mgr.Name(), version, err)
	}
}
