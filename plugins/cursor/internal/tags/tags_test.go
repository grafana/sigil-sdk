package tags

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuild(t *testing.T) {
	cases := []struct {
		name   string
		extras map[string]string
		in     BuiltinInputs
		check  func(t *testing.T, got map[string]string)
	}{
		{
			name: "builtins overwrite extras",
			extras: map[string]string{
				"git.branch": "fake",
				"cwd":        "fake-cwd",
				"keep":       "ok",
			},
			in: BuiltinInputs{
				WorkspaceRoot: "/ws",
				Cwd:           "/real-cwd",
				GitBranch:     "main",
			},
			check: func(t *testing.T, got map[string]string) {
				if got["git.branch"] != "main" {
					t.Errorf("built-in git.branch must win; got %q", got["git.branch"])
				}
				if got["cwd"] != "/real-cwd" {
					t.Errorf("built-in cwd must win; got %q", got["cwd"])
				}
				if got["keep"] != "ok" {
					t.Errorf("non-builtin extra must pass through; got %q", got["keep"])
				}
			},
		},
		{
			name: "cwd falls back to workspace root",
			in:   BuiltinInputs{WorkspaceRoot: "/ws"},
			check: func(t *testing.T, got map[string]string) {
				if got["cwd"] != "/ws" {
					t.Errorf("cwd should fall back to workspace root; got %q", got["cwd"])
				}
			},
		},
		{
			name: "no inputs returns nil",
			in:   BuiltinInputs{},
			check: func(t *testing.T, got map[string]string) {
				if got != nil {
					t.Errorf("Build with no inputs must return nil; got %v", got)
				}
			},
		},
		{
			name: "subagent set when background",
			in:   BuiltinInputs{IsBackgroundAgent: true, WorkspaceRoot: "/ws"},
			check: func(t *testing.T, got map[string]string) {
				if got["subagent"] != "true" {
					t.Errorf("subagent should be set; got %q", got["subagent"])
				}
			},
		},
		{
			name: "subagent absent when not background",
			in:   BuiltinInputs{WorkspaceRoot: "/ws"},
			check: func(t *testing.T, got map[string]string) {
				if _, ok := got["subagent"]; ok {
					t.Errorf("subagent should be absent for non-background agent")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, Build(tc.extras, tc.in))
		})
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

func TestResolveGitBranch(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (workspaceRoot string)
		want  string
	}{
		{
			name: "regular repo",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				writeFile(t, filepath.Join(root, ".git/HEAD"), "ref: refs/heads/feature/fancy\n")
				return root
			},
			want: "feature/fancy",
		},
		{
			name: "detached HEAD returns 12-char prefix",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				writeFile(t, filepath.Join(root, ".git/HEAD"), "abcdef0123456789abcdef0123456789abcdef01\n")
				return root
			},
			want: "abcdef012345",
		},
		{
			name: "gitdir indirection (worktree)",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				actualGitDir := filepath.Join(root, "actual-git")
				writeFile(t, filepath.Join(root, "wt/.git"), "gitdir: ../actual-git\n")
				writeFile(t, filepath.Join(actualGitDir, "HEAD"), "ref: refs/heads/wt-branch\n")
				return filepath.Join(root, "wt")
			},
			want: "wt-branch",
		},
		{
			name: "walks up parent directories",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				writeFile(t, filepath.Join(root, ".git/HEAD"), "ref: refs/heads/main\n")
				deep := filepath.Join(root, "a", "b", "c")
				if err := os.MkdirAll(deep, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return deep
			},
			want: "main",
		},
		{
			name: "no .git found",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			want: "",
		},
		{
			name: "empty workspace root",
			setup: func(t *testing.T) string {
				return ""
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.setup(t)
			if got := ResolveGitBranch(root); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
