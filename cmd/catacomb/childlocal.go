package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
)

var execCommand = exec.Command

const maxObserverBuffer = 1 << 20

type streamPeek struct {
	sessionID string
	costUSD   *float64
}

func (p *streamPeek) onLine(line []byte) {
	var e struct {
		Type         string   `json:"type"`
		SessionID    string   `json:"session_id"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
	}
	if json.Unmarshal(line, &e) != nil {
		return
	}
	if p.sessionID == "" && e.SessionID != "" {
		p.sessionID = e.SessionID
	}
	if e.Type == "result" && e.TotalCostUSD != nil {
		p.costUSD = e.TotalCostUSD
	}
}

type lineObserver struct {
	buf     []byte
	stopped bool
	observe func(line []byte)
}

func (w *lineObserver) Write(p []byte) (int, error) {
	if w.stopped {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.observe(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxObserverBuffer {
		w.buf = nil
		w.stopped = true
	}
	return len(p), nil
}

func (w *lineObserver) flush() {
	if w.stopped || len(w.buf) == 0 {
		return
	}
	w.observe(w.buf)
	w.buf = nil
}

func runChildLocal(stdout, stderr io.Writer, args []string, dir string, extraEnv []string, observe func(line []byte)) error {
	child := execCommand(args[0], args[1:]...)
	child.Stdin = os.Stdin
	child.Dir = dir
	child.Env = append(os.Environ(), extraEnv...)
	obs := &lineObserver{observe: observe}
	child.Stdout = io.MultiWriter(stdout, obs)
	child.Stderr = stderr
	err := child.Run()
	obs.flush()
	return err
}
