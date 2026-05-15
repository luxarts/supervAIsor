package state

import "testing"

func TestDecodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"-Users-you-Projects-supervAIsor": "/Users/you/Projects/supervAIsor",
		"-tmp-foo":                        "/tmp/foo",
		"":                                "",
	}
	for in, want := range cases {
		got := DecodeProjectDir(in)
		if got != want {
			t.Fatalf("DecodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeProjectDir_AbsolutePathPassthrough(t *testing.T) {
	cases := map[string]string{
		"/Users/you/Projects/ai-dream-team": "/Users/you/Projects/ai-dream-team",
		"/Users/you/Projects":               "/Users/you/Projects",
	}
	for in, want := range cases {
		got := DecodeProjectDir(in)
		if got != want {
			t.Fatalf("DecodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
