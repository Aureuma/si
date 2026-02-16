package main

import "testing"

func TestDisplayWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{name: "ascii", input: "hello", want: 5},
		{name: "ansi_stripped", input: "\x1b[31mhello\x1b[0m", want: 5},
		{name: "wide_cjk", input: "界", want: 2},
		{name: "combining_mark", input: "e\u0301", want: 1},
		{name: "emoji", input: "🙂", want: 2},
		{name: "emoji_variation_selector_dingbat", input: "❇️", want: 2},
		{name: "emoji_variation_selector_heart", input: "❤️", want: 2},
		{name: "dingbat_without_variation_selector", input: "❇", want: 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := displayWidth(tc.input); got != tc.want {
				t.Fatalf("displayWidth(%q)=%d want=%d", tc.input, got, tc.want)
			}
		})
	}
}
