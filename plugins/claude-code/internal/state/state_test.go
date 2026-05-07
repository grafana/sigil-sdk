package state

import (
	"os"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := Load("nonexistent-session")
	if s.Offset != 0 {
		t.Errorf("Offset = %d, want 0", s.Offset)
	}
	if s.Title != "" {
		t.Errorf("Title = %q, want empty", s.Title)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	want := Session{Offset: 12345, Title: "Fix the authentication bug", Model: "claude-sonnet-4-6"}
	if err := Save("test-session", want); err != nil {
		t.Fatal(err)
	}

	got := Load("test-session")
	if got.Offset != want.Offset {
		t.Errorf("Offset = %d, want %d", got.Offset, want.Offset)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
	if got.Model != want.Model {
		t.Errorf("Model = %q, want %q", got.Model, want.Model)
	}
}

func TestSave_UpdatesExisting(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := Save("s1", Session{Offset: 100, Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := Save("s1", Session{Offset: 200, Title: "second"}); err != nil {
		t.Fatal(err)
	}

	got := Load("s1")
	if got.Offset != 200 {
		t.Errorf("Offset = %d, want 200", got.Offset)
	}
	if got.Title != "second" {
		t.Errorf("Title = %q, want %q", got.Title, "second")
	}
}

func TestSave_TitleWithNewlines(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	want := Session{Offset: 100, Title: "title with\nnewlines\nin it"}
	if err := Save("s1", want); err != nil {
		t.Fatal(err)
	}

	got := Load("s1")
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
}

func TestLoad_CorruptedFile(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_STATE_HOME", d)

	// Write a corrupted state file (not valid JSON)
	sd := dir()
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path("bad"), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := Load("bad")
	if s.Offset != 0 {
		t.Errorf("Offset = %d, want 0 for corrupted file", s.Offset)
	}
}

func TestSanitizeSessionID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal-session-id", "normal-session-id"},
		{"../../etc/cron.d/evil", "____etc_cron.d_evil"},
		{"path/traversal", "path_traversal"},
		{"back\\slash", "back_slash"},
	}

	for _, tt := range tests {
		got := SanitizeSessionID(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeSessionID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
