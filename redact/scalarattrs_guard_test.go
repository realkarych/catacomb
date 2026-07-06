package redact_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
)

func nextSeq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func requireScalarTopLevelAttrs(t *testing.T, parser string, obs []model.Observation) {
	t.Helper()
	require.NotEmpty(t, obs, "parser %s must produce observations for this fixture", parser)
	for _, o := range obs {
		for k, v := range o.Attrs {
			switch v.(type) {
			case string, bool, int, int64, uint64, float64:
			default:
				t.Fatalf("redact.redactAttrs redacts only top-level scalar attr values: parser %s emitted non-scalar attr %q (%T) on kind %q — nested attr values would bypass write-path redaction; extend redactAttrs to walk nested values before landing this parser change", parser, k, v, o.Kind)
			}
		}
	}
}

func TestIngestParsersEmitOnlyScalarTopLevelAttrs(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"list files"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","isSidechain":true,"version":"1.2.3","cwd":"/home/kesha","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.txt","is_error":false}]}}`,
	}, "\n") + "\n"
	obs, _, err := jsonl.Parse(strings.NewReader(lines), "e1", nextSeq(), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	requireScalarTopLevelAttrs(t, "ingest/jsonl", obs)
}
