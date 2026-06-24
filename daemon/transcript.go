package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

func (d *Daemon) handleTranscript(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("catacomb: transcript handler recovered: %v", rec)
		}
	}()
	sc := bufio.NewScanner(r.Body)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	var currentSession string
	for sc.Scan() {
		line := sc.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		buf := make([]byte, len(trimmed))
		copy(buf, trimmed)
		if s := transcriptSessionID(buf); s != "" {
			currentSession = s
		}
		_ = d.IngestTranscript(buf, currentSession)
	}
	if err := sc.Err(); err != nil {
		log.Printf("catacomb: transcript scan: %v", err)
	}
	w.WriteHeader(http.StatusOK)
}

func transcriptSessionID(line []byte) string {
	var e struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return ""
	}
	return e.SessionID
}
