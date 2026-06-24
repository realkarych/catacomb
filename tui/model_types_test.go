package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionSummaryDecode(t *testing.T) {
	raw := `{"session":"s1","status":"ok","tokens_in":10,"tokens_out":5,"cost_usd":0.12,"cost_source":"reported","node_count":3,"tool_count":1,"error_count":0,"run_ids":["r1"],"counts_by_type":{"tool_call":1},"counts_by_status":{"ok":3},"error_rate":0}`
	var s SessionSummary
	require.NoError(t, json.Unmarshal([]byte(raw), &s))
	assert.Equal(t, "s1", s.Session)
	assert.Equal(t, "ok", s.Status)
	assert.Equal(t, int64(10), s.TokensIn)
	assert.Equal(t, int64(5), s.TokensOut)
	require.NotNil(t, s.CostUSD)
	assert.InDelta(t, 0.12, *s.CostUSD, 1e-9)
	assert.Equal(t, "reported", s.CostSource)
	assert.Equal(t, 3, s.NodeCount)
	assert.Equal(t, 1, s.ToolCount)
	assert.Equal(t, 0, s.ErrorCount)
	assert.Equal(t, []string{"r1"}, s.RunIDs)
	assert.Equal(t, 1, s.CountsByType["tool_call"])
	assert.Equal(t, 3, s.CountsByStatus["ok"])
	assert.Equal(t, float64(0), s.ErrorRate)
}

func TestSseEventDecode(t *testing.T) {
	raw := `{"kind":"node_upsert","rev":7,"execution_id":"e1","node":{"id":"n1","run_id":"r1","type":"tool_call","status":"ok","tokens_in":3}}`
	var ev SseEvent
	require.NoError(t, json.Unmarshal([]byte(raw), &ev))
	assert.Equal(t, "node_upsert", ev.Kind)
	assert.Equal(t, uint64(7), ev.Rev)
	assert.Equal(t, "e1", ev.ExecutionID)
	require.NotNil(t, ev.Node)
	assert.Equal(t, "n1", ev.Node.ID)
	assert.Equal(t, "r1", ev.Node.RunID)
	assert.Equal(t, "tool_call", ev.Node.Type)
	assert.Equal(t, "ok", ev.Node.Status)
	require.NotNil(t, ev.Node.TokensIn)
	assert.Equal(t, int64(3), *ev.Node.TokensIn)
}

func TestPayloadViewDecode(t *testing.T) {
	raw := `{"node_id":"n1","payload_hash":"h","input":{"a":1},"redactions":[{"path":"$.x","reason":"secret"}],"redacted":true}`
	var p PayloadView
	require.NoError(t, json.Unmarshal([]byte(raw), &p))
	assert.Equal(t, "n1", p.NodeID)
	assert.Equal(t, "h", p.PayloadHash)
	assert.True(t, p.Redacted)
	require.Len(t, p.Redactions, 1)
	assert.Equal(t, "$.x", p.Redactions[0].Path)
	assert.Equal(t, "secret", p.Redactions[0].Reason)
}

func TestNodeDecode(t *testing.T) {
	raw := `{"id":"n1","run_id":"r1","type":"session","parent_id":"p1","name":"nm","status":"running","t_start":"2026-01-01T00:00:00Z","t_end":"2026-01-01T00:01:00Z","duration_ms":60000,"tokens_in":100,"tokens_out":200,"cost_usd":0.5,"attrs":{"cost_source":"reported"},"payload_hash":"ph","sources":[{"source":"obs","obs_id":"oid","observed_at":"2026-01-01T00:00:00Z"}],"tier":"t1","rev":42}`
	var n Node
	require.NoError(t, json.Unmarshal([]byte(raw), &n))
	assert.Equal(t, "n1", n.ID)
	assert.Equal(t, "r1", n.RunID)
	assert.Equal(t, "session", n.Type)
	assert.Equal(t, "p1", n.ParentID)
	assert.Equal(t, "nm", n.Name)
	assert.Equal(t, "running", n.Status)
	require.NotNil(t, n.TStart)
	assert.Equal(t, "2026-01-01T00:00:00Z", *n.TStart)
	require.NotNil(t, n.TEnd)
	assert.Equal(t, "2026-01-01T00:01:00Z", *n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(60000), *n.DurationMS)
	require.NotNil(t, n.TokensIn)
	assert.Equal(t, int64(100), *n.TokensIn)
	require.NotNil(t, n.TokensOut)
	assert.Equal(t, int64(200), *n.TokensOut)
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 0.5, *n.CostUSD, 1e-9)
	assert.Equal(t, "reported", n.Attrs["cost_source"])
	assert.Equal(t, "ph", n.PayloadHash)
	require.Len(t, n.Sources, 1)
	assert.Equal(t, "obs", n.Sources[0].Source)
	assert.Equal(t, "oid", n.Sources[0].ObsID)
	assert.Equal(t, "t1", n.Tier)
	assert.Equal(t, uint64(42), n.Rev)
}

func TestEdgeDecode(t *testing.T) {
	raw := `{"id":"e1","run_id":"r1","type":"parent_child","src":"n1","dst":"n2","rev":5}`
	var e Edge
	require.NoError(t, json.Unmarshal([]byte(raw), &e))
	assert.Equal(t, "e1", e.ID)
	assert.Equal(t, "r1", e.RunID)
	assert.Equal(t, "parent_child", e.Type)
	assert.Equal(t, "n1", e.Src)
	assert.Equal(t, "n2", e.Dst)
	assert.Equal(t, uint64(5), e.Rev)
}

func TestSseEventWithEdgeDecode(t *testing.T) {
	raw := `{"kind":"edge_upsert","rev":3,"edge":{"id":"e1","run_id":"r","type":"sequence","src":"a","dst":"b","rev":3},"old_id":"old","new_id":"new"}`
	var ev SseEvent
	require.NoError(t, json.Unmarshal([]byte(raw), &ev))
	assert.Equal(t, "edge_upsert", ev.Kind)
	require.NotNil(t, ev.Edge)
	assert.Equal(t, "e1", ev.Edge.ID)
	assert.Equal(t, "old", ev.OldID)
	assert.Equal(t, "new", ev.NewID)
}

func TestSessionSummaryOptionalFields(t *testing.T) {
	raw := `{"session":"s2","status":"running","tokens_in":0,"tokens_out":0,"cost_source":"","node_count":0,"tool_count":0,"error_count":0,"run_ids":[],"counts_by_type":{},"counts_by_status":{},"error_rate":0,"started_at":"2026-01-01T00:00:00Z","ended_at":"2026-01-01T01:00:00Z","duration_ms":3600000,"model_id":"claude-opus"}`
	var s SessionSummary
	require.NoError(t, json.Unmarshal([]byte(raw), &s))
	assert.Equal(t, "s2", s.Session)
	assert.Equal(t, "2026-01-01T00:00:00Z", s.StartedAt)
	assert.Equal(t, "2026-01-01T01:00:00Z", s.EndedAt)
	require.NotNil(t, s.DurationMS)
	assert.Equal(t, int64(3600000), *s.DurationMS)
	assert.Equal(t, "claude-opus", s.ModelID)
}
