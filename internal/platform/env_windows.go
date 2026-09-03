//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// --- User environment variables (HKCU\Environment) ---

func (w *windowsPlatform) SetUserEnv(key, value string) error {
	if strings.EqualFold(key, "Path") {
		return errors.New("PATH must be changed through the PATH functions")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(key, value); err != nil {
		return err
	}
	w.BroadcastPathChange()
	return nil
}

func (w *windowsPlatform) UnsetUserEnv(key string) error {
	if strings.EqualFold(key, "Path") {
		return errors.New("PATH must be changed through the PATH functions")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(key); err != nil && err != registry.ErrNotExist {
		return err
	}
	w.BroadcastPathChange()
	return nil
}

func (w *windowsPlatform) GetUserEnv(key string) (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envRegKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(key)
	if err != nil {
		return "", false
	}
	return val, true
}
