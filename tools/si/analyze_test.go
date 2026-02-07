package main

import "testing"

func TestNormalizeAnalyzeModuleFilter(t *testing.T) {
	cases := map[string]string{
		"tools/si":   "tools/si",
		"./tools/si": "tools/si",
		" tools/si ": "tools/si",
		".":          "",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeAnalyzeModuleFilter(in); got != want {
			t.Fatalf("normalizeAnalyzeModuleFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAnalyzeModules_NoFilterReturnsAll(t *testing.T) {
	all := []analyzeModule{
		{Rel: "agents/shared", Dir: "/repo/agents/shared"},
		{Rel: "tools/si", Dir: "/repo/tools/si"},
	}
	got, err := resolveAnalyzeModules(all, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(all) {
		t.Fatalf("expected %d modules, got %d", len(all), len(got))
	}
}

func TestResolveAnalyzeModules_FilterByRelativePath(t *testing.T) {
	all := []analyzeModule{
		{Rel: "agents/shared", Dir: "/repo/agents/shared"},
		{Rel: "tools/si", Dir: "/repo/tools/si"},
	}
	got, err := resolveAnalyzeModules(all, []string{"tools/si"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Rel != "tools/si" {
		t.Fatalf("unexpected modules: %+v", got)
	}
}

func TestResolveAnalyzeModules_FilterByBasename(t *testing.T) {
	all := []analyzeModule{
		{Rel: "agents/shared", Dir: "/repo/agents/shared"},
		{Rel: "tools/si", Dir: "/repo/tools/si"},
	}
	got, err := resolveAnalyzeModules(all, []string{"shared"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Rel != "agents/shared" {
		t.Fatalf("unexpected modules: %+v", got)
	}
}

func TestResolveAnalyzeModules_UnknownFilter(t *testing.T) {
	all := []analyzeModule{{Rel: "tools/si", Dir: "/repo/tools/si"}}
	if _, err := resolveAnalyzeModules(all, []string{"missing"}); err == nil {
		t.Fatalf("expected unknown module error")
	}
}

func TestResolveAnalyzeModules_AmbiguousBasename(t *testing.T) {
	all := []analyzeModule{
		{Rel: "tools/si", Dir: "/repo/tools/si"},
		{Rel: "agents/si", Dir: "/repo/agents/si"},
	}
	if _, err := resolveAnalyzeModules(all, []string{"si"}); err == nil {
		t.Fatalf("expected ambiguous module error")
	}
}
