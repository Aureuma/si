package main

import "testing"

func TestParseCLIHelp(t *testing.T) {
	help, err := parseCLI([]string{"--help"})
	if err != nil {
		t.Fatalf("parseCLI --help: %v", err)
	}
	if !help {
		t.Fatalf("expected help=true")
	}
}

func TestParseCLIUnexpectedArgs(t *testing.T) {
	help, err := parseCLI([]string{"extra"})
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if help {
		t.Fatalf("expected help=false")
	}
}
