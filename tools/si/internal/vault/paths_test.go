package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace", in: "  ", want: ""},
		{name: "tilde", in: "~", want: tempHome},
		{name: "tilde child", in: "~/secrets", want: filepath.Join(tempHome, "secrets")},
		{name: "no tilde", in: "/opt/si", want: "/opt/si"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandHome(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCleanAbs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace", in: "  ", wantErr: true},
		{name: "relative", in: "configs", want: filepath.Clean(filepath.Join(cwd, "configs"))},
		{name: "absolute", in: "/opt/si/../si", want: filepath.Clean("/opt/si/../si")},
		{name: "home", in: "~", want: tempHome},
		{name: "home child", in: "~/vault", want: filepath.Join(tempHome, "vault")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CleanAbs(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
