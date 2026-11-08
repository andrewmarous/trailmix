//go:build !darwin && !linux

package learning

import "os/exec"

func prepareProcess(cmd *exec.Cmd) {}
