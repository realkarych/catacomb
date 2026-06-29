//go:build !windows

package main

import (
	"os"
	"syscall"
)

func signalProcess(pid int, sig syscall.Signal) error {
	p, _ := os.FindProcess(pid)
	return p.Signal(sig)
}
