//go:build !windows

package transfer

import "os/exec"

func hide(*exec.Cmd) {}
