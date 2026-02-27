package main

import "testing"

func TestApplyVivaConfigSet(t *testing.T) {
	settings := defaultSettings()
	changed, err := applyVivaConfigSet(&settings, vivaConfigSetInput{
		RepoProvided:  true,
		Repo:          "/tmp/viva",
		BinProvided:   true,
		Bin:           "/tmp/viva/bin/viva",
		BuildProvided: true,
		BuildRaw:      "true",
	})
	if err != nil {
		t.Fatalf("applyVivaConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Viva.Repo != "/tmp/viva" || settings.Viva.Bin != "/tmp/viva/bin/viva" {
		t.Fatalf("unexpected viva repo/bin: %#v", settings.Viva)
	}
	if settings.Viva.Build == nil || !*settings.Viva.Build {
		t.Fatalf("expected viva.build=true")
	}
}

func TestApplyVivaConfigSetClearsBuildOnEmpty(t *testing.T) {
	settings := defaultSettings()
	settings.Viva.Build = boolPtr(true)
	changed, err := applyVivaConfigSet(&settings, vivaConfigSetInput{BuildProvided: true, BuildRaw: ""})
	if err != nil {
		t.Fatalf("applyVivaConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Viva.Build != nil {
		t.Fatalf("expected build unset")
	}
}

func TestApplyVivaConfigSetRejectsInvalidBuild(t *testing.T) {
	settings := defaultSettings()
	_, err := applyVivaConfigSet(&settings, vivaConfigSetInput{BuildProvided: true, BuildRaw: "bad"})
	if err == nil {
		t.Fatalf("expected invalid build error")
	}
}
