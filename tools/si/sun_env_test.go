package main

import "testing"

func TestSunEnvAliasesPreferSunThenHelia(t *testing.T) {
	t.Setenv("SI_SUN_BASE_URL", "https://sun.example")
	t.Setenv("SI_HELIA_BASE_URL", "https://helia.example")
	t.Setenv("SI_SUN_TOKEN", "sun-token")
	t.Setenv("SI_HELIA_TOKEN", "helia-token")
	t.Setenv("SI_SUN_TASKBOARD", "sun-board")
	t.Setenv("SI_HELIA_TASKBOARD", "helia-board")
	t.Setenv("SI_SUN_TASKBOARD_AGENT", "sun-agent")
	t.Setenv("SI_HELIA_TASKBOARD_AGENT", "helia-agent")
	t.Setenv("SI_SUN_TASKBOARD_LEASE_SECONDS", "77")
	t.Setenv("SI_HELIA_TASKBOARD_LEASE_SECONDS", "88")
	t.Setenv("SI_SUN_MACHINE_ID", "sun-machine")
	t.Setenv("SI_HELIA_MACHINE_ID", "helia-machine")
	t.Setenv("SI_SUN_OPERATOR_ID", "sun-operator")
	t.Setenv("SI_HELIA_OPERATOR_ID", "helia-operator")
	t.Setenv("SI_SUN_ALLOW_INSECURE_HTTP", "1")
	t.Setenv("SI_HELIA_ALLOW_INSECURE_HTTP", "0")

	if got := envSunBaseURL(); got != "https://sun.example" {
		t.Fatalf("envSunBaseURL=%q", got)
	}
	if got := envSunToken(); got != "sun-token" {
		t.Fatalf("envSunToken=%q", got)
	}
	if got := envSunTaskboard(); got != "sun-board" {
		t.Fatalf("envSunTaskboard=%q", got)
	}
	if got := envSunTaskboardAgent(); got != "sun-agent" {
		t.Fatalf("envSunTaskboardAgent=%q", got)
	}
	if got := envSunTaskboardLeaseSeconds(); got != "77" {
		t.Fatalf("envSunTaskboardLeaseSeconds=%q", got)
	}
	if got := envSunMachineID(); got != "sun-machine" {
		t.Fatalf("envSunMachineID=%q", got)
	}
	if got := envSunOperatorID(); got != "sun-operator" {
		t.Fatalf("envSunOperatorID=%q", got)
	}
	if !envSunAllowInsecureHTTP() {
		t.Fatalf("expected insecure-http override true")
	}
}

func TestSunEnvAliasesFallbackToHelia(t *testing.T) {
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_HELIA_BASE_URL", "https://helia.example")
	t.Setenv("SI_SUN_TOKEN", "")
	t.Setenv("SI_HELIA_TOKEN", "helia-token")

	if got := envSunBaseURL(); got != "https://helia.example" {
		t.Fatalf("envSunBaseURL fallback=%q", got)
	}
	if got := envSunToken(); got != "helia-token" {
		t.Fatalf("envSunToken fallback=%q", got)
	}
}
