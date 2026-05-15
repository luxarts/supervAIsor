package hostname

import "testing"

func TestClean_StripsDotLocal(t *testing.T) {
	if got := Clean("my-mbp.local"); got != "my-mbp" {
		t.Errorf("Clean = %q, want my-mbp", got)
	}
}

func TestClean_LeavesOthersAlone(t *testing.T) {
	if got := Clean("my-mbp"); got != "my-mbp" {
		t.Errorf("Clean = %q, want my-mbp", got)
	}
	if got := Clean("server.example.com"); got != "server.example.com" {
		t.Errorf("Clean = %q, want server.example.com", got)
	}
}

func TestResolve_ReturnsNonEmpty(t *testing.T) {
	got, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if got == "" {
		t.Fatal("Resolve returned empty hostname")
	}
}
