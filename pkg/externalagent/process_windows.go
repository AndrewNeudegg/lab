//go:build windows

package externalagent

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

func terminateProcessGroup(pid int) error {
	return nil
}
