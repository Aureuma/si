package main

import (
	"reflect"
	"testing"
)

func TestParseCloudflareParams(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  map[string]string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  map[string]string{},
		},
		{
			name:  "trims and skips blanks",
			input: []string{"  ", "\t", " a = 1 ", "b=2"},
			want:  map[string]string{"a": "1", "b": "2"},
		},
		{
			name:  "skips invalid entries",
			input: []string{"noequals", "=1", " a = 2 "},
			want:  map[string]string{"a": "2"},
		},
		{
			name:  "keeps last duplicate",
			input: []string{"a=1", "b=2", "a=3"},
			want:  map[string]string{"a": "3", "b": "2"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseCloudflareParams(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCloudflareParams() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseCloudflareBodyParams(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  map[string]any
	}{
		{
			name:  "decodes json primitives and structures",
			input: []string{"a=1", "b= true ", "c=[1,2]", "d={\"k\":\"v\"}", "e=null"},
			want: map[string]any{
				"a": "1",
				"b": true,
				"c": []any{float64(1), float64(2)},
				"d": map[string]any{"k": "v"},
				"e": nil,
			},
		},
		{
			name:  "keeps invalid json as string",
			input: []string{"a={bad", "b=[1,"},
			want:  map[string]any{"a": "{bad", "b": "[1,"},
		},
		{
			name:  "empty value preserved",
			input: []string{"a=", "b=  "},
			want:  map[string]any{"a": "", "b": ""},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseCloudflareBodyParams(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCloudflareBodyParams() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
