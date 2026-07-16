package codex

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
)

const (
	basicFixture   = "testdata/basic.jsonl"
	basicSessionID = "019f6b85-627f-7be3-81dc-ae8563860180"
	basicPrompt    = "Reply with exactly: hello"
)

type parseCase struct {
	name      string
	file      string
	input     string
	mainRunID string
	wantErr   bool
	check     func(t *testing.T, obs []model.Observation, dc drift.Counts)
}

func seqFor(t *testing.T) func() uint64 {
	t.Helper()
	var n uint64
	return func() uint64 {
		s := n
		n++
		return s
	}
}

func identityObservedAt(ts time.Time) time.Time { return ts }

func openInput(t *testing.T, tc parseCase) io.Reader {
	t.Helper()
	if tc.file == "" {
		return strings.NewReader(tc.input)
	}
	f, err := os.Open(tc.file)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func byKind(obs []model.Observation, kind string) []model.Observation {
	var out []model.Observation
	for _, o := range obs {
		if o.Kind == kind {
			out = append(out, o)
		}
	}
	return out
}

type errReader struct{ read bool }

func (r *errReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("read fail")
	}
	r.read = true
	p[0] = '\n'
	return 1, nil
}

func TestParse(t *testing.T) {
	cases := []parseCase{
		{
			name: "basic fixture user prompt",
			file: basicFixture,
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				up := byKind(obs, "user_prompt")
				require.Len(t, up, 1)
				require.NotNil(t, up[0].Payload)
				assert.JSONEq(t, `"Reply with exactly: hello"`, string(up[0].Payload.Input))
				assert.Empty(t, up[0].Payload.Output)
				assert.NotEmpty(t, up[0].Payload.Hash)
				assert.Equal(t, "human", up[0].Attrs["prompt_kind"])
				assert.Equal(t, model.PromptUUID(basicSessionID, basicPrompt), up[0].Correlation.UUID)
				assert.Equal(t, time.Date(2026, 7, 16, 15, 22, 15, 302000000, time.UTC), up[0].EventTime)
			},
		},
		{
			name: "basic fixture assistant turn",
			file: basicFixture,
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, "T1", turns[0].Correlation.MessageID)
				assert.Equal(t, "gpt-5.4-mini", turns[0].Attrs["model"])
				assert.Equal(t, int64(11663), turns[0].Attrs["tokens_in"])
				assert.Equal(t, int64(16), turns[0].Attrs["tokens_out"])
				assert.Equal(t, int64(5504), turns[0].Attrs["cache_read_in"])
				assert.Equal(t, int64(5875), turns[0].Attrs["duration_ms"])
				require.NotNil(t, turns[0].Payload)
				assert.JSONEq(t, `"hello"`, string(turns[0].Payload.Output))
				assert.Empty(t, turns[0].Payload.Input)
				assert.NotEmpty(t, turns[0].Payload.Hash)
				assert.Equal(t, time.Date(2026, 7, 16, 15, 22, 17, 450000000, time.UTC), turns[0].EventTime)
			},
		},
		{
			name: "basic fixture stamping and identity",
			file: basicFixture,
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				require.Len(t, obs, 2)
				seen := map[string]bool{}
				for i, o := range obs {
					assert.Equal(t, basicSessionID, o.RunID)
					assert.Equal(t, basicSessionID, o.Correlation.SessionID)
					assert.Equal(t, "exec-C", o.ExecutionID)
					assert.Equal(t, model.SourceJSONL, o.Source)
					assert.Equal(t, "codex", o.Attrs["agent_runtime"])
					assert.Equal(t, "0.144.4", o.Attrs["codex_version"])
					assert.Equal(t, "/work/codex-probe", o.Attrs["cwd"])
					assert.Equal(t, uint64(i), o.Seq)
					assert.Equal(t, o.EventTime, o.ObservedAt)
					assert.NotEmpty(t, o.ObsID)
					assert.False(t, seen[o.ObsID])
					seen[o.ObsID] = true
				}
			},
		},
		{
			name: "basic fixture drift",
			file: basicFixture,
			check: func(t *testing.T, _ []model.Observation, dc drift.Counts) {
				assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
			},
		},
		{
			name: "empty reader",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Empty(t, dc)
			},
		},
		{
			name:  "blank lines only",
			input: "\n   \n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Empty(t, dc)
			},
		},
		{
			name:    "invalid json line",
			input:   "{not json}\n",
			wantErr: true,
		},
		{
			name:    "invalid session_meta payload",
			input:   `{"type":"session_meta","payload":5}` + "\n",
			wantErr: true,
		},
		{
			name:    "invalid turn_context payload",
			input:   `{"type":"turn_context","payload":[]}` + "\n",
			wantErr: true,
		},
		{
			name:    "invalid response_item payload",
			input:   `{"type":"response_item","payload":"x"}` + "\n",
			wantErr: true,
		},
		{
			name:    "invalid event_msg payload",
			input:   `{"type":"event_msg","payload":3}` + "\n",
			wantErr: true,
		},
		{
			name: "token_count keeps last non-null info",
			input: `{"type":"turn_context","payload":{"turn_id":"T1","model":"m"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1,"cached_input_tokens":2,"output_tokens":3}}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"cached_input_tokens":20,"output_tokens":30}}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":null}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":{}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"T1","duration_ms":7}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, int64(10), turns[0].Attrs["tokens_in"])
				assert.Equal(t, int64(30), turns[0].Attrs["tokens_out"])
				assert.Equal(t, int64(20), turns[0].Attrs["cache_read_in"])
				assert.Equal(t, int64(7), turns[0].Attrs["duration_ms"])
				assert.Empty(t, dc)
			},
		},
		{
			name: "missing task_complete still emits turn without duration",
			input: `{"type":"turn_context","payload":{"turn_id":"T1","model":"gpt-5.4-mini"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":5,"cached_input_tokens":0,"output_tokens":6}}}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, "T1", turns[0].Correlation.MessageID)
				assert.Equal(t, "gpt-5.4-mini", turns[0].Attrs["model"])
				assert.Equal(t, int64(5), turns[0].Attrs["tokens_in"])
				_, ok := turns[0].Attrs["duration_ms"]
				assert.False(t, ok)
			},
		},
		{
			name:  "garbage timestamp bumps drift",
			input: `{"timestamp":"not-a-timestamp","type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				require.Len(t, obs, 1)
				assert.True(t, obs[0].EventTime.IsZero())
				assert.Equal(t, drift.Counts{drift.ReasonBadTimestamp: 1}, dc)
			},
		},
		{
			name:  "missing timestamp no drift",
			input: `{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				require.Len(t, obs, 1)
				assert.True(t, obs[0].EventTime.IsZero())
				assert.Empty(t, dc)
			},
		},
		{
			name: "mainRunID pins run id",
			input: `{"type":"session_meta","payload":{"session_id":"child-1","cli_version":"0.144.4","cwd":"/w"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n",
			mainRunID: "main-9",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				require.Len(t, obs, 1)
				assert.Equal(t, "main-9", obs[0].RunID)
				assert.Equal(t, "child-1", obs[0].Correlation.SessionID)
				assert.Equal(t, model.PromptUUID("child-1", "hi"), obs[0].Correlation.UUID)
			},
		},
		{
			name: "session_meta backfills session id from id",
			input: `{"type":"session_meta","payload":{"id":"only-id"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				require.Len(t, obs, 1)
				assert.Equal(t, "only-id", obs[0].RunID)
			},
		},
		{
			name:  "unknown event_msg payload type bumps drift",
			input: `{"type":"event_msg","payload":{"type":"quantum_delta"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
			},
		},
		{
			name:  "unknown response_item payload type bumps drift",
			input: `{"type":"response_item","payload":{"type":"hologram"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
			},
		},
		{
			name: "known event_msg types skipped without drift",
			input: `{"type":"event_msg","payload":{"type":"agent_message","message":"hi"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"mcp_tool_call_begin"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"mcp_tool_call_end"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"error"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"session_error"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"stream_error"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"turn_aborted"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"context_compacted"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"exec_command_begin"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"exec_command_end"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"patch_apply_begin"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"patch_apply_end"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Empty(t, dc)
			},
		},
		{
			name: "known response_item types skipped without drift",
			input: `{"type":"response_item","payload":{"type":"reasoning","encrypted_content":"gAAA"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"c1"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"tool_search_call"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"tool_search_output"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"web_search_call"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"mcp_tool_call"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Empty(t, dc)
			},
		},
		{
			name: "compacted and world_state skipped without drift",
			input: `{"type":"compacted","payload":{"message":"m"}}` + "\n" +
				`{"type":"world_state","payload":{"full":true}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, dc drift.Counts) {
				assert.Empty(t, obs)
				assert.Empty(t, dc)
			},
		},
		{
			name:  "empty user_message emits nothing",
			input: `{"type":"event_msg","payload":{"type":"user_message","message":""}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				assert.Empty(t, obs)
			},
		},
		{
			name: "assistant message without metadata attributes to current turn",
			input: `{"type":"turn_context","payload":{"turn_id":"T2","model":"m"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, "T2", turns[0].Correlation.MessageID)
				require.NotNil(t, turns[0].Payload)
				assert.JSONEq(t, `"partial"`, string(turns[0].Payload.Output))
				_, ok := turns[0].Attrs["duration_ms"]
				assert.False(t, ok)
			},
		},
		{
			name: "assistant message with empty text keeps earlier final text",
			input: `{"type":"turn_context","payload":{"turn_id":"T3","model":"m"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final"}],"internal_chat_message_metadata_passthrough":{"turn_id":"T3"}}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[],"internal_chat_message_metadata_passthrough":{"turn_id":"T3"}}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"T3","duration_ms":1}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				require.NotNil(t, turns[0].Payload)
				assert.JSONEq(t, `"final"`, string(turns[0].Payload.Output))
			},
		},
		{
			name: "duplicate task_complete emits one turn",
			input: `{"type":"turn_context","payload":{"turn_id":"T4","model":"m"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"T4","duration_ms":1}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"T4","duration_ms":2}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, int64(1), turns[0].Attrs["duration_ms"])
			},
		},
		{
			name: "turn_started alias groups tokens without session_meta",
			input: `{"type":"event_msg","payload":{"type":"turn_started","turn_id":"T5"}}` + "\n" +
				`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":7,"cached_input_tokens":0,"output_tokens":8}}}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				turns := byKind(obs, "assistant_turn")
				require.Len(t, turns, 1)
				assert.Equal(t, "T5", turns[0].Correlation.MessageID)
				assert.Equal(t, int64(7), turns[0].Attrs["tokens_in"])
				assert.Equal(t, "", turns[0].RunID)
				_, hasModel := turns[0].Attrs["model"]
				assert.False(t, hasModel)
				_, hasVersion := turns[0].Attrs["codex_version"]
				assert.False(t, hasVersion)
				_, hasCwd := turns[0].Attrs["cwd"]
				assert.False(t, hasCwd)
				assert.Equal(t, "codex", turns[0].Attrs["agent_runtime"])
			},
		},
		{
			name:  "synthetic prompt kind",
			input: `{"type":"event_msg","payload":{"type":"user_message","message":"<system-reminder>tidy up"}}` + "\n",
			check: func(t *testing.T, obs []model.Observation, _ drift.Counts) {
				up := byKind(obs, "user_prompt")
				require.Len(t, up, 1)
				assert.Equal(t, "system", up[0].Attrs["prompt_kind"])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs, dc, err := Parse(openInput(t, tc), tc.mainRunID, "exec-C", seqFor(t), identityObservedAt)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tc.check(t, obs, dc)
		})
	}
}

func TestParseScannerError(t *testing.T) {
	_, _, err := Parse(&errReader{}, "", "exec-C", seqFor(t), identityObservedAt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex.Parse:")
}

func TestParseLineTooLongWrapsErrTooLong(t *testing.T) {
	r := strings.NewReader(strings.Repeat("a", 16*1024*1024+1))
	_, _, err := Parse(r, "", "exec-C", seqFor(t), identityObservedAt)
	require.Error(t, err)
	assert.ErrorIs(t, err, bufio.ErrTooLong)
}

func TestParseUsesInjectedSeqAndObservedAt(t *testing.T) {
	var n uint64 = 40
	next := func() uint64 { n++; return n }
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	obs, _, err := Parse(strings.NewReader(
		`{"timestamp":"2026-07-16T15:22:15.302Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`+"\n"),
		"", "exec-Z", next, func(time.Time) time.Time { return at })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, uint64(41), obs[0].Seq)
	assert.Equal(t, at, obs[0].ObservedAt)
	assert.Equal(t, time.Date(2026, 7, 16, 15, 22, 15, 302000000, time.UTC), obs[0].EventTime)
}
