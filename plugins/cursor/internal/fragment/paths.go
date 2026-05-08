package fragment

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	appDir         = "sigil-cursor"
	fragmentPrefix = "gen-"
	fragmentSuffix = ".json"
)

// StateRoot returns the root state directory.
// Honors XDG_STATE_HOME, falls back to $HOME/.local/state, then OS tempdir.
func StateRoot() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, appDir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), appDir)
	}
	return filepath.Join(home, ".local", "state", appDir)
}

// ConversationDir is the directory holding all fragments for one conversation.
// Cursor's payload IDs are UUIDs; we trust them as filename components.
func ConversationDir(conversationID string) string {
	return filepath.Join(StateRoot(), conversationID)
}

// SessionFilePath is where session metadata is stored after sessionStart.
func SessionFilePath(conversationID string) string {
	return filepath.Join(ConversationDir(conversationID), "session.json")
}

// FragmentFilePath is the JSON file for one accumulating generation.
func FragmentFilePath(conversationID, generationID string) string {
	return filepath.Join(
		ConversationDir(conversationID),
		fragmentPrefix+generationID+fragmentSuffix,
	)
}

// ParseFragmentFilename returns the generation_id encoded in a fragment
// filename, or "" if the entry isn't a fragment.
func ParseFragmentFilename(entry string) string {
	if !strings.HasPrefix(entry, fragmentPrefix) || !strings.HasSuffix(entry, fragmentSuffix) {
		return ""
	}
	return entry[len(fragmentPrefix) : len(entry)-len(fragmentSuffix)]
}

// LogFilePath is where SIGIL_DEBUG=true writes its log.
func LogFilePath() string {
	return filepath.Join(StateRoot(), "sigil-cursor.log")
}
