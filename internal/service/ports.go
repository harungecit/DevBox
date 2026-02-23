package service

import (
	"fmt"
	"net"
	"time"
)

// IsPortAvailable checks if a TCP port is available for binding
func IsPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// IsPortInUse checks if something is listening on a TCP port
func IsPortInUse(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// FindAvailablePort finds the first available port starting from preferred
func FindAvailablePort(preferred int) int {
	if IsPortAvailable(preferred) {
		return preferred
	}
	// Try incrementing
	for i := 1; i <= 100; i++ {
		candidate := preferred + i
		if IsPortAvailable(candidate) {
			return candidate
		}
	}
	return preferred // Fallback
}

// PortStatus returns a human-readable status for a port
type PortStatus struct {
	Port      int    `json:"port"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

// WaitForPortRelease polls until a port is no longer in use.
// timeoutSec is the maximum number of seconds to wait.
func WaitForPortRelease(port int, timeoutSec int) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if !IsPortInUse(port) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// CheckPort returns detailed port status
func CheckPort(port int) PortStatus {
	if IsPortAvailable(port) {
		return PortStatus{Port: port, Available: true, Message: "available"}
	}
	return PortStatus{Port: port, Available: false, Message: fmt.Sprintf("port %d is already in use", port)}
}
