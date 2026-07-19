package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSuccessReturnsZeroAndWritesResultToStdout(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"version"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Equal(t, "catacomb "+Version+"\n", out.String())
	assert.Empty(t, errBuf.String())
}

func TestRunUnknownSubcommandReturnsOneAndNamesTheSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"does-not-exist"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "does-not-exist")
	assert.Empty(t, out.String())
}

func TestRunExitCodeContractSeparatesGateFailureFromOperationalFailure(t *testing.T) {
	regressing := t.TempDir()
	writeTokenEvidenceRun(t, regressing, "base-0", "base", 10)
	writeTokenEvidenceRun(t, regressing, "cand-0", "cand", 5000)
	clean := evidenceRoot(t)

	cases := []struct {
		name          string
		args          []string
		wantCode      int
		wantStderr    bool
		wantStdoutHas string
	}{
		{
			name:          "clean comparison exits 0 and stays silent on stderr",
			args:          []string{"regress", "--runs-dir", clean, "--baseline", "label:variant=base", "--candidate", "label:variant=base"},
			wantCode:      0,
			wantStderr:    false,
			wantStdoutHas: "overall",
		},
		{
			name:          "detected regression exits 1 and reports on stdout only",
			args:          []string{"regress", "--runs-dir", regressing, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--min-support", "1"},
			wantCode:      1,
			wantStderr:    false,
			wantStdoutHas: "overall regression",
		},
		{
			name:       "operational failure exits 2 and reports on stderr only",
			args:       []string{"replay", filepath.Join(t.TempDir(), "nope.jsonl")},
			wantCode:   2,
			wantStderr: true,
		},
		{
			name:       "unknown flag exits 1",
			args:       []string{"regress", "--runs-dir", clean, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--bogus-flag"},
			wantCode:   1,
			wantStderr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := run(tc.args, &out, &errBuf)
			require.Equal(t, tc.wantCode, code, "stdout=%q stderr=%q", out.String(), errBuf.String())
			if tc.wantStderr {
				assert.NotEmpty(t, errBuf.String())
			} else {
				assert.Empty(t, errBuf.String())
			}
			if tc.wantStdoutHas != "" {
				assert.Contains(t, out.String(), tc.wantStdoutHas)
			}
		})
	}
}
