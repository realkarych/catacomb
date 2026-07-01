//go:build darwin

package main

import "golang.org/x/sys/unix"

var sysctlKinfoProc = unix.SysctlKinfoProc

func processStartTime(pid int) (int64, error) {
	kp, err := sysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	tv := kp.Proc.P_starttime
	return tv.Sec*1_000_000 + int64(tv.Usec), nil
}
