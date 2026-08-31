//go:build ignore

// Manual diagnostic: replicates the exact in-app update launch path
// (goroutine → platform.LaunchInstaller("/S") → 1.5s sleep → exit) to
// verify the silent installer completes when launched the way DevBox does.
// Run: go run cmd/updatetest/main.go <path-to-setup.exe>
package main

import (
	"fmt"
	"os"
	"time"

	"DevBox/internal/platform"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: updatetest <setup.exe>")
		os.Exit(2)
	}
	done := make(chan error, 1)
	go func() {
		done <- platform.LaunchInstaller(os.Args[1], "/S")
	}()
	if err := <-done; err != nil {
		fmt.Println("LaunchInstaller error:", err)
		os.Exit(1)
	}
	fmt.Println("LaunchInstaller returned OK; sleeping 1.5s then exiting (like DevBox)")
	time.Sleep(1500 * time.Millisecond)
	fmt.Println("exiting now")
}
