//go:build !windows

package transcribe

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
