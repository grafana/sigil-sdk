// Package launcher holds the small pieces shared by the agent CLI launchers:
// the execve handoff that replaces the sigil process with the target CLI, and
// a command runner that captures stdout while surfacing stderr on failure.
//
// Each launcher keeps its own lookPath/execFn/runInstall/pluginList package
// vars — the test seams — so these helpers take the exec function and command
// arguments as parameters instead of reaching for package globals. Install-flow
// orchestration stays in each launcher because the CLIs differ (single vs
// marketplace+install steps, per-step error wording).
package launcher

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// ExecFunc matches syscall.Exec: it replaces the current process image with a
// new program. The launchers assign syscall.Exec to a package var and pass it
// here so tests can substitute a recording stub.
type ExecFunc func(argv0 string, argv []string, envv []string) error

// Exec replaces the current process with bin, prepending bin as argv[0] and
// forwarding args plus env. name is used only for the error prefix
// ("exec <name>"). Callers pass the env explicitly so local-mode launches can
// inject SIGIL_ENDPOINT overrides via local.Environ; pass os.Environ() for the
// normal path. On success the process is replaced and Exec does not return; a
// returned error means the execve syscall itself failed.
func Exec(execFn ExecFunc, bin, name string, args, env []string) error {
	argv := append([]string{bin}, args...)
	if err := execFn(bin, argv, env); err != nil {
		return fmt.Errorf("exec %s: %w", name, err)
	}
	return nil
}

// Output runs `bin args...` and returns its stdout. On failure it attaches any
// captured stderr to the error: *exec.ExitError renders only "exit status N"
// under %v and drops the CLI's own diagnostic, so we surface it explicitly.
func Output(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}
