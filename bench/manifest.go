package bench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type ManifestEntry struct {
	RunID      string    `json:"run_id"`
	Task       string    `json:"task"`
	Variant    string    `json:"variant"`
	Rep        int       `json:"rep"`
	ExitCode   int       `json:"exit_code"`
	SessionID  string    `json:"session_id,omitempty"`
	Marked     bool      `json:"marked"`
	BasketHash string    `json:"basket_hash"`
	FinishedAt time.Time `json:"finished_at"`
	Note       string    `json:"note,omitempty"`
}

type Manifest struct {
	Path string
}

func (m Manifest) Append(e ManifestEntry) error {
	f, err := os.OpenFile(m.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("bench.Manifest.Append: %w", err)
	}
	encErr := json.NewEncoder(f).Encode(e)
	closeErr := f.Close()
	return appendResult(encErr, closeErr)
}

func appendResult(encErr, closeErr error) error {
	if encErr != nil {
		return fmt.Errorf("bench.Manifest.Append: %w", encErr)
	}
	if closeErr != nil {
		return fmt.Errorf("bench.Manifest.Append: %w", closeErr)
	}
	return nil
}

func (m Manifest) Completed() (map[string]ManifestEntry, error) {
	out := map[string]ManifestEntry{}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("bench.Manifest.Completed: %w", err)
	}
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var e ManifestEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("bench.Manifest.Completed: %w", err)
		}
		out[e.RunID] = e
	}
	return out, nil
}
