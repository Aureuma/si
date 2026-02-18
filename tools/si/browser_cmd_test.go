package main

import "testing"

func TestDefaultBrowserConfigUsesSINetworkFallback(t *testing.T) {
	t.Setenv("SI_BROWSER_NETWORK", "")
	t.Setenv("SI_NETWORK", "si-custom-net")
	cfg := defaultBrowserConfig()
	if cfg.Network != "si-custom-net" {
		t.Fatalf("defaultBrowserConfig().Network=%q want=%q", cfg.Network, "si-custom-net")
	}
}

func TestDefaultBrowserConfigPrefersSIBrowserNetwork(t *testing.T) {
	t.Setenv("SI_NETWORK", "si-net-a")
	t.Setenv("SI_BROWSER_NETWORK", "si-net-b")
	cfg := defaultBrowserConfig()
	if cfg.Network != "si-net-b" {
		t.Fatalf("defaultBrowserConfig().Network=%q want=%q", cfg.Network, "si-net-b")
	}
}
