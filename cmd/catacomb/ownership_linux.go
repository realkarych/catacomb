//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var (
	readProcStat    = os.ReadFile
	readBootID      = os.ReadFile
	errProcStatForm = errors.New("ownership: malformed /proc stat")
)

func bootID() string {
	b, err := readBootID("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func processStartTime(pid int) (int64, error) {
	b, err := readProcStat(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 > len(b) {
		return 0, errProcStatForm
	}
	fields := strings.Fields(string(b[i+1:]))
	if len(fields) < 20 {
		return 0, errProcStatForm
	}
	v, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, errProcStatForm
	}
	return v, nil
}
