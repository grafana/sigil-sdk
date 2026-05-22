package launcher

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestExecForwardsArgvAndEnv(t *testing.T) {
	var gotArgv0 string
	var gotArgv, gotEnv []string
	execFn := func(argv0 string, argv []string, envv []string) error {
		gotArgv0 = argv0
		gotArgv = argv
		gotEnv = envv
		return nil
	}

	args := []string{"exec", "--model", "gpt-5", "hi"}
	env := []string{"SIGIL_ENDPOINT=http://127.0.0.1:9999", "PATH=/usr/bin"}
	if err := Exec(execFn, "/usr/local/bin/codex", "codex", args, env); err != nil {
		t.Fatalf("Exec returned err: %v", err)
	}
	if gotArgv0 != "/usr/local/bin/codex" {
		t.Errorf("argv0 = %q", gotArgv0)
	}
	wantArgv := append([]string{"/usr/local/bin/codex"}, args...)
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Errorf("argv = %v; want %v", gotArgv, wantArgv)
	}
	if !reflect.DeepEqual(gotEnv, env) {
		t.Errorf("env = %v; want %v", gotEnv, env)
	}
}

func TestExecWrapsFailureWithName(t *testing.T) {
	cases := []struct{ name string }{{"codex"}, {"copilot"}, {"claude"}, {"pi"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execFn := func(string, []string, []string) error { return errors.New("boom") }
			err := Exec(execFn, "/bin/"+tc.name, tc.name, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "exec "+tc.name) {
				t.Fatalf("err = %v; want contains %q", err, "exec "+tc.name)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Fatalf("err = %v; want underlying error wrapped", err)
			}
		})
	}
}

func TestOutput(t *testing.T) {
	cases := []struct {
		name       string
		needsUnix  bool
		bin        string
		args       []string
		wantErr    bool
		wantStdout string
		wantErrSub string
	}{
		{
			name:       "returns stdout",
			needsUnix:  true,
			bin:        "/bin/echo",
			args:       []string{"hello"},
			wantStdout: "hello",
		},
		{
			name:       "attaches stderr on failure",
			needsUnix:  true,
			bin:        "/bin/sh",
			args:       []string{"-c", "echo diag 1>&2; exit 3"},
			wantErr:    true,
			wantErrSub: "diag",
		},
		{
			name:    "missing binary errors",
			bin:     "/no/such/binary-xyz",
			args:    []string{"arg"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsUnix && runtime.GOOS == "windows" {
				t.Skip("uses unix shell utilities")
			}
			out, err := Output(context.Background(), tc.bin, tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.wantErrSub != "" && !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("err = %v; want substring %q", err, tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("Output: %v", err)
			}
			if tc.wantStdout != "" && strings.TrimSpace(string(out)) != tc.wantStdout {
				t.Fatalf("stdout = %q; want %q", out, tc.wantStdout)
			}
		})
	}
}
