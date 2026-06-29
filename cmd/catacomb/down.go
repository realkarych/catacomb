package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	downStopInterval = 100 * time.Millisecond
	downStopAttempts = 50
)

var (
	downSignal = signalProcess
	downSleep  = time.Sleep
)

func signalProcess(pid int, sig syscall.Signal) error {
	p, _ := os.FindProcess(pid)
	return p.Signal(sig)
}

func waitGone(pid int) bool {
	for i := 0; i < downStopAttempts; i++ {
		if err := downSignal(pid, syscall.Signal(0)); err != nil {
			return true
		}
		downSleep(downStopInterval)
	}
	return false
}

func stopDaemon(pid int, force bool) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	if err := downSignal(pid, syscall.Signal(0)); err != nil {
		return false, nil
	}
	if err := downSignal(pid, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	if !force {
		return false, ErrDaemonStop
	}
	if err := downSignal(pid, syscall.SIGKILL); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	return false, ErrDaemonStop
}
