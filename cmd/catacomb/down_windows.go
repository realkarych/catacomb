//go:build windows

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func signalProcess(pid int, sig syscall.Signal) error {
	if sig == syscall.Signal(0) {
		p, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		_ = p.Release()
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}
