//go:build !windows && !darwin

package main

// DevBox ships for Windows and macOS. This entry point only exists so the
// package compiles for `go vet` / `go test` on Linux CI runners.
func main() {
	println("DevBox is available for Windows and macOS only.")
}
