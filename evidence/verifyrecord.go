package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const verifyFileName = "verify.json"

type VerifyRecord struct {
	Cmd        []string  `json:"cmd"`
	SHA256     string    `json:"sha256"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
	Mode       string    `json:"mode"`
	FinishedAt time.Time `json:"finished_at"`
	Error      string    `json:"error,omitempty"`
}

func VerifyConfigSHA256(cmd []string, env map[string]string) string {
	data, _ := json.Marshal(struct {
		Cmd []string          `json:"cmd"`
		Env map[string]string `json:"env"`
	}{Cmd: cmd, Env: env})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func WriteVerify(dir string, r VerifyRecord) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence.WriteVerify: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, verifyFileName), data, 0o600); err != nil {
		return fmt.Errorf("evidence.WriteVerify: %w", err)
	}
	return nil
}

func ReadVerify(dir string) (VerifyRecord, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, verifyFileName))
	if errors.Is(err, os.ErrNotExist) {
		return VerifyRecord{}, false, nil
	}
	if err != nil {
		return VerifyRecord{}, false, fmt.Errorf("evidence.ReadVerify: %w", err)
	}
	var r VerifyRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return VerifyRecord{}, false, fmt.Errorf("evidence.ReadVerify: %w", err)
	}
	return r, true, nil
}
