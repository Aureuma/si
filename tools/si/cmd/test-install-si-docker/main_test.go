package main

import (
	"strings"
	"testing"
)

func TestParseCLIHelp(t *testing.T) {
	help, err := parseCLI([]string{"--help"})
	if err != nil {
		t.Fatalf("parseCLI --help error: %v", err)
	}
	if !help {
		t.Fatalf("expected help=true")
	}
}

func TestParseCLIUnexpectedArgs(t *testing.T) {
	help, err := parseCLI([]string{"extra"})
	if err == nil {
		t.Fatalf("expected error for unexpected args")
	}
	if help {
		t.Fatalf("expected help=false")
	}
	if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := loadConfig("/tmp/root", func(string) string { return "" })
	if cfg.RootDir != "/tmp/root" {
		t.Fatalf("unexpected root: %q", cfg.RootDir)
	}
	if cfg.SmokeImage != "si-install-smoke:local" {
		t.Fatalf("unexpected smoke image: %q", cfg.SmokeImage)
	}
	if cfg.NonrootImage != "si-install-nonroot:local" {
		t.Fatalf("unexpected nonroot image: %q", cfg.NonrootImage)
	}
	if cfg.SourceDir != "/tmp/root" {
		t.Fatalf("unexpected source dir: %q", cfg.SourceDir)
	}
	if cfg.SkipNonroot {
		t.Fatalf("expected skip nonroot false by default")
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	env := map[string]string{
		"SI_INSTALL_SMOKE_IMAGE":        "custom-smoke",
		"SI_INSTALL_NONROOT_IMAGE":      "custom-nonroot",
		"SI_INSTALL_SOURCE_DIR":         "/repo/source",
		"SI_INSTALL_SMOKE_SKIP_NONROOT": "1",
	}
	cfg := loadConfig("/tmp/root", func(k string) string { return env[k] })
	if cfg.SmokeImage != "custom-smoke" {
		t.Fatalf("unexpected smoke image: %q", cfg.SmokeImage)
	}
	if cfg.NonrootImage != "custom-nonroot" {
		t.Fatalf("unexpected nonroot image: %q", cfg.NonrootImage)
	}
	if cfg.SourceDir != "/repo/source" {
		t.Fatalf("unexpected source dir: %q", cfg.SourceDir)
	}
	if !cfg.SkipNonroot {
		t.Fatalf("expected skip nonroot true")
	}
}
