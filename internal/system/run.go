// Package system integrates the panel with the underlying Debian/Ubuntu
// host: web server vhosts, DNS zones, mail maps, databases, system users,
// cron, firewall and service control.
//
// Every function degrades gracefully when the corresponding service is not
// installed, so the panel stays usable on partial installations and during
// development on non-Linux machines.
package system

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const defaultTimeout = 2 * time.Minute

// Linux reports whether we are running on a real target host.
func Linux() bool { return runtime.GOOS == "linux" }

// run executes a command with the default timeout and returns combined output.
func run(name string, args ...string) (string, error) {
	return runOpts(defaultTimeout, nil, name, args...)
}

// runOpts executes a command with a custom timeout and optional extra
// environment variables (appended to the current environment), returning
// combined output. Used for long-running, non-interactive work like apt.
func runOpts(timeout time.Duration, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		out := strings.TrimSpace(buf.String())
		if err != nil {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
		}
		return out, nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return "", fmt.Errorf("%s: timed out", name)
	}
}

// runStdin executes a command, writing stdin to its standard input, and returns
// its standard output only. stderr is captured separately and folded into the
// error on failure. Keeping stdout clean matters for commands whose output we
// parse but which may also print prompts to stderr (e.g. `doveadm pw`). Passing
// secrets on stdin instead of argv keeps them out of the process list, where
// other local users — including tenants' PHP-FPM workers — could read them from
// /proc (see SECURITY_AUDIT F-14).
func runStdin(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// runCapture executes a command in dir with stdin, capturing stdout and stderr
// separately and enforcing a timeout. Unlike run/runStdin it returns both
// streams and the raw error regardless of exit status, so callers (e.g. function
// test invocations) can show the function's structured output and its logs
// independently and report a non-zero exit as a failed run rather than an error.
func runCapture(timeout time.Duration, dir, stdin, name string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(timeout):
		cmd.Process.Kill()
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return out.String(), errb.String(), err
}

// have reports whether a binary exists in PATH.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
