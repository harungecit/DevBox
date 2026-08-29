package project

import (
	"fmt"
	"strings"

	"DevBox/internal/platform"
)

const devboxMarker = "# DevBox managed"

// AddHostsEntry adds a domain->127.0.0.1 mapping to the hosts file via elevated privileges.
func AddHostsEntry(domain string) error {
	data, err := platform.ReadHostsFile()
	if err != nil {
		return fmt.Errorf("cannot read hosts file: %w", err)
	}

	// Already mapped? Match whole host names, not substrings — otherwise
	// "app.test" would be considered present when "my-app.test" is.
	if hostsFileDomains()[strings.ToLower(domain)] {
		return nil
	}

	entry := fmt.Sprintf("127.0.0.1 %s %s", domain, devboxMarker)
	content := strings.TrimRight(string(data), "\r\n") + "\r\n" + entry + "\r\n"

	return platform.WriteHostsFileElevated([]byte(content))
}

// RemoveHostsEntry removes a domain from the hosts file via elevated privileges.
func RemoveHostsEntry(domain string) error {
	data, err := platform.ReadHostsFile()
	if err != nil {
		return fmt.Errorf("cannot read hosts file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var filtered []string
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, domain) && strings.Contains(trimmed, devboxMarker) {
			found = true
			continue
		}
		filtered = append(filtered, line)
	}

	if !found {
		return nil
	}

	return platform.WriteHostsFileElevated([]byte(strings.Join(filtered, "\n")))
}

// ListDevBoxHosts returns all DevBox-managed host entries.
func ListDevBoxHosts() []string {
	data, err := platform.ReadHostsFile()
	if err != nil {
		return nil
	}

	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, devboxMarker) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				hosts = append(hosts, parts[1])
			}
		}
	}
	return hosts
}
