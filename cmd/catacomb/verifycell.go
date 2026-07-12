package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
)

const verifyStdoutCap = 1 << 20

type verifySpec struct {
	EvidenceDir string
	Workdir     string
	RunID       string
	Basket      string
	Task        string
	Variant     string
	Rep         int
	AgentExit   int
	Mode        string
	ExtraEnv    []string
}

func runVerifyCell(ctx context.Context, stderr io.Writer, v bench.Verify, spec verifySpec) evidence.VerifyRecord {
	rec := evidence.VerifyRecord{
		Cmd:    v.Cmd,
		SHA256: evidence.VerifyConfigSHA256(v.Cmd, canonicalEnv(v.Env)),
		Mode:   spec.Mode,
	}
	if d, _ := v.TimeoutDuration(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	start := nowFn()
	stdout, capped, runErr := runVerifierChild(ctx, stderr, v, spec)
	end := nowFn()
	rec.DurationMS = end.Sub(start).Milliseconds()
	rec.FinishedAt = end.UTC()
	code, _ := exitInfo(runErr)
	rec.ExitCode = code
	rec.Error = classifyVerify(ctx, spec, stdout, capped, runErr)
	if werr := evidence.WriteVerify(spec.EvidenceDir, rec); werr != nil {
		rec.Error = appendNote(rec.Error, werr.Error())
	}
	return rec
}

func classifyVerify(ctx context.Context, spec verifySpec, stdout []byte, capped bool, runErr error) string {
	switch {
	case runErr != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "timed out"
	case runErr != nil:
		return "verifier failed: " + runErr.Error()
	case capped:
		return fmt.Sprintf("verifier stdout exceeded %d bytes", verifyStdoutCap)
	default:
		return persistVerifierScores(spec.EvidenceDir, stdout, spec.RunID)
	}
}

func persistVerifierScores(dir string, stdout []byte, runID string) string {
	lines, err := parseVerifierScores(stdout, runID)
	if err != nil {
		return err.Error()
	}
	if err := writeVerifierScores(dir, lines); err != nil {
		return err.Error()
	}
	return ""
}

func parseVerifierScores(stdout []byte, runID string) ([][]byte, error) {
	var lines [][]byte
	for i, raw := range strings.Split(string(stdout), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		e, perr := parseScoreLine(line)
		if perr != nil {
			return nil, fmt.Errorf("verifier stdout line %d: %w", i+1, perr)
		}
		lines = append(lines, scoreLineWithRunID(line, e.RunID, runID))
	}
	return lines, nil
}

func scoreLineWithRunID(line, present, fallback string) []byte {
	if present != "" {
		return []byte(line)
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal([]byte(line), &obj)
	id, _ := json.Marshal(fallback)
	obj["run_id"] = id
	out, _ := json.Marshal(obj)
	return out
}

func writeVerifierScores(dir string, lines [][]byte) error {
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "scores.jsonl"), buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("verifier scores: %w", err)
	}
	return nil
}

func runVerifierChild(ctx context.Context, stderr io.Writer, v bench.Verify, spec verifySpec) ([]byte, bool, error) {
	child := execCommandContext(ctx, v.Cmd[0], v.Cmd[1:]...)
	child.Dir = verifyCwd(spec)
	child.Env = verifyEnv(v, spec)
	sink := &capWriter{limit: verifyStdoutCap}
	child.Stdout = sink
	child.Stderr = stderr
	child.WaitDelay = 10 * time.Second
	err := child.Run()
	return sink.buf.Bytes(), sink.over, err
}

func verifyCwd(spec verifySpec) string {
	if spec.Mode == "offline" {
		return spec.EvidenceDir
	}
	return spec.Workdir
}

func verifyEnv(v bench.Verify, spec verifySpec) []string {
	env := os.Environ()
	env = append(env, spec.ExtraEnv...)
	for k, val := range v.Env {
		env = append(env, k+"="+val)
	}
	return append(env,
		"CATACOMB_EVIDENCE_DIR="+spec.EvidenceDir,
		"CATACOMB_WORKDIR="+workdirEnv(spec),
		"CATACOMB_RUN_ID="+spec.RunID,
		"CATACOMB_BASKET="+spec.Basket,
		"CATACOMB_TASK="+spec.Task,
		"CATACOMB_VARIANT="+spec.Variant,
		"CATACOMB_REP="+strconv.Itoa(spec.Rep),
		"CATACOMB_AGENT_EXIT_CODE="+strconv.Itoa(spec.AgentExit),
	)
}

func workdirEnv(spec verifySpec) string {
	if spec.Mode == "offline" {
		return ""
	}
	return spec.Workdir
}

func canonicalEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	return env
}

type capWriter struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if !w.over && w.buf.Len()+len(p) > w.limit {
		w.over = true
	}
	if w.over {
		return len(p), nil
	}
	return w.buf.Write(p)
}
