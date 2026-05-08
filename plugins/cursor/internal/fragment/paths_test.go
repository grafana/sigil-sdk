package fragment

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRoot_XDGOverride(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	if got := StateRoot(); got != "/custom/state/sigil-cursor" {
		t.Errorf("got %q want /custom/state/sigil-cursor", got)
	}
}

func TestFragmentFilePath_Layout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x")
	got := FragmentFilePath("conv-uuid", "gen-id")
	want := filepath.Join("/x", "sigil-cursor", "conv-uuid", "gen-gen-id.json")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestParseFragmentFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"gen-abc.json", "abc"},
		{"gen-.json", ""},
		{"session.json", ""},
		{"gen-abc.txt", ""},
		{"abc.json", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ParseFragmentFilename(tc.in); got != tc.want {
				t.Errorf("ParseFragmentFilename(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSessionFilePath_LooksRight(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x")
	got := SessionFilePath("conv1")
	if !strings.HasSuffix(got, "/sigil-cursor/conv1/session.json") {
		t.Errorf("got %q does not have expected suffix", got)
	}
}
