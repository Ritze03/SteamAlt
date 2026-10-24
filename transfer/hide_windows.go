//go:build windows

package transfer

import (
	"os/exec"
	"syscall"
)

// hide stops a console window from flashing when a -H windowsgui build execs
// a console program. 0x08000000 is CREATE_NO_WINDOW.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
