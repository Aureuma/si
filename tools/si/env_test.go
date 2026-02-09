package main

import (
	"reflect"
	"testing"
)

func TestFilterEnv(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty input",
			input: []string{},
			want:  nil,
		},
		{
			name:  "trims and drops blanks",
			input: []string{"  ", "\t", " A=1 ", "B=2"},
			want:  []string{"A=1", "B=2"},
		},
		{
			name:  "keeps last duplicate",
			input: []string{"A=1", "B=1", "A=2"},
			want:  []string{"A=2", "B=1"},
		},
		{
			name:  "handles key without equals",
			input: []string{"A", "A=2", "B=1", "B"},
			want:  []string{"A=2", "B"},
		},
		{
			name:  "skips empty key",
			input: []string{"=1", " =2 ", "C=3"},
			want:  []string{"C=3"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := filterEnv(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("filterEnv() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
