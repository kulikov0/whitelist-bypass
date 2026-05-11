//go:build windows

package wintunnel

import (
	"os/exec"
	"syscall"
)

// hideConsole sets CREATE_NO_WINDOW so netsh/route/powershell helpers
// do not flash a cmd window when the joiner runs under Electron.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
