//go:build !unix

package verify

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}
