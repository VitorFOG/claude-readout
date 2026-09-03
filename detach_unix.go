//go:build !windows

package main

import "syscall"

// detachAttrs puts the refresh child in its own session so it outlives the
// statusline process and never holds Claude Code's pipes open.
func detachAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
