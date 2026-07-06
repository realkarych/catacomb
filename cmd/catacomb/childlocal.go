package main

import (
	"encoding/json"
	"io"
	"os"
)

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
