package main

import (
	"syscall"

	"github.com/realkarych/catacomb/daemon"
)

var (
	ownershipAlive     = func(pid int) bool { return downSignal(pid, syscall.Signal(0)) == nil }
	ownershipStartTime = processStartTime
	ownershipBootID    = bootID
	daemonOwned        = isOurLiveDaemon
)

func isOurLiveDaemon(disc daemon.Discovery) bool {
	if disc.Pid <= 0 {
		return false
	}
	if !ownershipAlive(disc.Pid) {
		return false
	}
	if disc.StartToken == 0 {
		return true
	}
	tok, err := ownershipStartTime(disc.Pid)
	if err != nil {
		return false
	}
	if tok != disc.StartToken {
		return false
	}
	return disc.BootID == ownershipBootID()
}
