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
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Linux reports whether we are running on a real target host.
func Linux() bool { return runtime.GOOS == "linux" }

// run executes a command with a timeout and returns combined output.
func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
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
	case <-time.After(2 * time.Minute):
		cmd.Process.Kill()
		return "", fmt.Errorf("%s: timed out", name)
	}
}

// have reports whether a binary exists in PATH.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
