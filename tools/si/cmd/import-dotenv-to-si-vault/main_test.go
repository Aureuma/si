package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInferTargetEnv(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: ".env", want: "dev"},
		{in: ".env.prod", want: "prod"},
		{in: ".env.PRODUCTION.local", want: "prod"},
		{in: ".env.development", want: "dev"},
	}
	for _, tc := range cases {
		if got := inferTargetEnv(tc.in); got != tc.want {
			t.Fatalf("inferTargetEnv(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestParseDotenv(t *testing.T) {
	input := `
# comment
export FOO=bar
BAD-KEY=bad
QUOTED_SINGLE='hello world'
QUOTED_DOUBLE="line1\nline2"
RAW="unterminated
SPACE_KEY = value
`
	got := parseDotenv(input)
	want := map[string]string{
		"FOO":           "bar",
		"QUOTED_SINGLE": "hello world",
		"QUOTED_DOUBLE": "line1\nline2",
		"SPACE_KEY":     "value",
		"RAW":           "\"unterminated",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDotenv mismatch\n got=%#v\nwant=%#v", got, want)
	}
}

func TestListEnvFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{".env", ".env.dev", ".env.prod", ".env.keys", ".env.vault", "notenv", ".envdir"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		if name == ".envdir" {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	got, err := listEnvFiles(dir)
	if err != nil {
		t.Fatalf("listEnvFiles: %v", err)
	}
	want := []string{
		filepath.Join(dir, ".env"),
		filepath.Join(dir, ".env.dev"),
		filepath.Join(dir, ".env.prod"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listEnvFiles mismatch\n got=%#v\nwant=%#v", got, want)
	}
}
