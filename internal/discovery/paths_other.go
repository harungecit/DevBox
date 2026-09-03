//go:build !windows && !darwin

package discovery

// Discovery only knows well-known install locations on Windows and macOS.
// Other platforms (linux CI builds) fall back to PATH-based detection only.

func runtimeCandidateRoots(name string) []string { return nil }

func serviceCandidateRoots(name string) []string { return nil }

func composerCandidatePhars() []string { return nil }
