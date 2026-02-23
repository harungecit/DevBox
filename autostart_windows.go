package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	autoStartRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartValueName   = "DevBox"
)

// setWindowsAutoStart adds or removes DevBox from Windows startup via registry
func setWindowsAutoStart(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		return key.SetStringValue(autoStartValueName, `"`+exePath+`"`)
	}

	// Disable: delete the value (ignore error if it doesn't exist)
	err = key.DeleteValue(autoStartValueName)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to remove auto-start entry: %w", err)
	}
	return nil
}
