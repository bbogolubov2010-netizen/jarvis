//go:build !windows

package appscan

import (
	"context"
	"os/exec"
)

func execCommand(path string, args ...string) *exec.Cmd {
	return exec.Command(path, args...)
}

func launchAUMID(string) error {
	return errUnsupported
}

func scanStartMenu(context.Context) []App { return nil }

func scanAppPaths() []App { return nil }

func scanProgramFiles(context.Context) []App { return nil }
