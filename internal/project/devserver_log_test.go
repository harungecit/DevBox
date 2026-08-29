package project

import "testing"

func TestStripANSI(t *testing.T) {
	in := "\x1b[32m ready\x1b[0m - started server on 0.0.0.0:3000\x1b[?25h"
	if got := StripANSI(in); got != " ready - started server on 0.0.0.0:3000" {
		t.Fatalf("got %q", got)
	}
}

func TestLastMeaningfulLogLine(t *testing.T) {
	lines := []string{"Error: listen EADDRINUSE: address already in use :::3000", "  port: 3000", "}", "", "\x1b[?25h"}
	for i := range lines {
		lines[i] = StripANSI(lines[i])
	}
	if got := LastMeaningfulLogLine(lines); got != "port: 3000" {
		t.Fatalf("got %q", got)
	}
}
