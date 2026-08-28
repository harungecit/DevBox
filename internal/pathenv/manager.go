package pathenv

import "DevBox/internal/platform"

// GetUserPATH reads the user-level PATH entries.
func GetUserPATH() ([]string, error) {
	return platform.GetUserPATH()
}

// SetUserPATH writes the user-level PATH entries.
func SetUserPATH(entries []string) error {
	return platform.SetUserPATH(entries)
}

// AddToPath adds a directory to the user PATH if not already present.
func AddToPath(dir string) error {
	return platform.AddToPath(dir)
}

// RemoveFromPath removes a directory from the user PATH.
func RemoveFromPath(dir string) error {
	return platform.RemoveFromPath(dir)
}

// Contains checks if a directory is in the user PATH.
func Contains(dir string) bool {
	return platform.PathContains(dir)
}

// SwitchRuntimePath removes old runtime path and adds new one.
func SwitchRuntimePath(runtimeName, oldPath, newPath string) error {
	if oldPath != "" {
		if err := RemoveFromPath(oldPath); err != nil {
			return err
		}
	}
	if newPath != "" {
		return AddToPath(newPath)
	}
	return nil
}

// GetSystemPATH reads the system-level PATH (read-only for display).
func GetSystemPATH() ([]string, error) {
	return platform.GetSystemPATH()
}
