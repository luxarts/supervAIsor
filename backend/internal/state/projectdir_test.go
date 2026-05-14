package state

import "testing"

func TestDecodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"-Users-lucasbacelo-Projects-supervAIsor": "/Users/lucasbacelo/Projects/supervAIsor",
		"-tmp-foo":                                "/tmp/foo",
		"":                                        "",
	}
	for in, want := range cases {
		got := DecodeProjectDir(in)
		if got != want {
			t.Fatalf("DecodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
