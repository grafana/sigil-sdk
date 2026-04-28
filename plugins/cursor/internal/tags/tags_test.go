package tags

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuild_BuiltinsOverwriteExtras(t *testing.T) {
	got := Build(
		map[string]string{
			"git.branch": "fake",
			"cwd":        "fake-cwd",
			"keep":       "ok",
		},
		BuiltinInputs{
			WorkspaceRoot: "/ws",
			Cwd:           "/real-cwd",
			GitBranch:     "main",
		},
	)
	if got["git.branch"] != "main" {
		t.Errorf("built-in git.branch must win; got %q", got["git.branch"])
	}
	if got["cwd"] != "/real-cwd" {
		t.Errorf("built-in cwd must win; got %q", got["cwd"])
	}
	if got["keep"] != "ok" {
		t.Errorf("non-builtin extra must pass through; got %q", got["keep"])
	}
}

func TestBuild_CwdFallsBackToWorkspaceRoot(t *testing.T) {
	got := Build(nil, BuiltinInputs{WorkspaceRoot: "/ws"})
	if got["cwd"] != "/ws" {
		t.Errorf("cwd should fall back to workspace root; got %q", got["cwd"])
	}
}

func TestBuild_NoInputsReturnsNil(t *testing.T) {
	if got := Build(nil, BuiltinInputs{}); got != nil {
		t.Errorf("Build with no inputs must return nil; got %v", got)
	}
}

func TestBuild_SubagentOnlyWhenBackground(t *testing.T) {
	with := Build(nil, BuiltinInputs{IsBackgroundAgent: true, WorkspaceRoot: "/ws"})
	if with["subagent"] != "true" {
		t.Errorf("subagent should be set; got %q", with["subagent"])
	}
	without := Build(nil, BuiltinInputs{WorkspaceRoot: "/ws"})
	if _, ok := without["subagent"]; ok {
		t.Errorf("subagent should be absent for non-background agent")
	}
}

func TestParseExtra(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", nil},
		{"single", "k=v", map[string]string{"k": "v"}},
		{"multiple", "a=1,b=2", map[string]string{"a": "1", "b": "2"}},
		{"trims whitespace", "  k =  v  ", map[string]string{"k": "v"}},
		{"skips entry without equals", "bad", nil},
		{"skips empty key", "=v", nil},
		{"skips empty value", "k=", nil},
		{"skips mixed malformed", "ok=1,bad,=v,k=", map[string]string{"ok": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseExtra(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d len(want)=%d (got=%v)", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("got[%q]=%q want %q", k, got[k], v)
				}
			}
		})
	}
}

// writeFile creates a file with the given content under root, ensuring parent
// dirs exist. Test helper so HEAD/.git fixtures are easy to set up inline.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestResolveGitBranch_RegularRepo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git/HEAD"), "ref: refs/heads/feature/fancy\n")
	if got := ResolveGitBranch(root); got != "feature/fancy" {
		t.Errorf("got %q want %q", got, "feature/fancy")
	}
}

func TestResolveGitBranch_DetachedHEAD(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git/HEAD"), "abcdef0123456789abcdef0123456789abcdef01\n")
	if got := ResolveGitBranch(root); got != "abcdef012345" {
		t.Errorf("got %q want %q (12-char prefix)", got, "abcdef012345")
	}
}

func TestResolveGitBranch_GitdirIndirection(t *testing.T) {
	root := t.TempDir()
	// Worktree: .git is a file pointing to the actual git dir.
	actualGitDir := filepath.Join(root, "actual-git")
	writeFile(t, filepath.Join(root, "wt/.git"), "gitdir: ../actual-git\n")
	writeFile(t, filepath.Join(actualGitDir, "HEAD"), "ref: refs/heads/wt-branch\n")
	if got := ResolveGitBranch(filepath.Join(root, "wt")); got != "wt-branch" {
		t.Errorf("got %q want %q", got, "wt-branch")
	}
}

func TestResolveGitBranch_WalksUp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git/HEAD"), "ref: refs/heads/main\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := ResolveGitBranch(deep); got != "main" {
		t.Errorf("got %q want %q (should walk up to find .git)", got, "main")
	}
}

func TestResolveGitBranch_NoRepo(t *testing.T) {
	if got := ResolveGitBranch(t.TempDir()); got != "" {
		t.Errorf("got %q want empty (no .git anywhere)", got)
	}
}

func TestResolveGitBranch_EmptyWorkspaceRoot(t *testing.T) {
	if got := ResolveGitBranch(""); got != "" {
		t.Errorf("got %q want empty", got)
	}
}
