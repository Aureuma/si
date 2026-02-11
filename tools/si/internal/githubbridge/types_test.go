package githubbridge

import "testing"

func TestParseAuthMode(t *testing.T) {
	cases := []struct {
		in   string
		want AuthMode
		ok   bool
	}{
		{in: "app", want: AuthModeApp, ok: true},
		{in: "oauth", want: AuthModeOAuth, ok: true},
		{in: "token", want: AuthModeOAuth, ok: true},
		{in: "pat", want: AuthModeOAuth, ok: true},
		{in: "", ok: false},
		{in: "wat", ok: false},
	}
	for _, tc := range cases {
		got, err := ParseAuthMode(tc.in)
		if tc.ok {
			if err != nil {
				t.Fatalf("ParseAuthMode(%q) err=%v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseAuthMode(%q)=%q want %q", tc.in, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("ParseAuthMode(%q) expected error", tc.in)
		}
	}
}
