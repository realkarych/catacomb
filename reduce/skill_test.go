package reduce

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func TestSkillNodeType(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s1", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"superpowers:brainstorming"}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s1")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "superpowers:brainstorming", n.Name)
}

func TestSlashCommandNodeTypeAndCleanName(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s2", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "SlashCommand"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"command":"/code-review high"}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s2")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "code-review", n.Name)
}

func TestSkillNameFallbackNoPayload(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s3", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s3")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "Skill", n.Name)
}

func TestSkillNameFallbackBadJSON(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s4", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{bad`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s4")]
	assert.Equal(t, "Skill", n.Name)
}

func TestSkillTypeUpgradeReversedOrder(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	res := ob("tool_result", "toolu_s5", t0)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	use := ob("assistant_tool_use", "toolu_s5", t1)
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"verify"}`)}
	g := NewGraph()
	g.ApplyAll([]model.Observation{res, use})
	n := g.Nodes[model.ToolCallID(execID, "toolu_s5")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "verify", n.Name)
}

func TestSlashCommandEmptyCommandFallback(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s6", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "SlashCommand"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"command":""}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s6")]
	assert.Equal(t, "SlashCommand", n.Name)
}

func TestSkillStrongNameOverridesLiteralRegardlessOfSeq(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	literal := ob("assistant_tool_use", "toolu_m1a", t0)
	literal.Seq = 1
	literal.Attrs = map[string]any{"name": "Skill"}

	real := ob("assistant_tool_use", "toolu_m1a", t0)
	real.Seq = 2
	real.Attrs = map[string]any{"name": "Skill"}
	real.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"superpowers:verify"}`)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{literal, real})
	n := g.Nodes[model.ToolCallID(execID, "toolu_m1a")]
	assert.Equal(t, "superpowers:verify", n.Name)

	g2 := NewGraph()
	g2.ApplyAll([]model.Observation{real, literal})
	n2 := g2.Nodes[model.ToolCallID(execID, "toolu_m1a")]
	assert.Equal(t, "superpowers:verify", n2.Name)
}

func TestSkillWeakNameDoesNotOverrideStrong(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	real := ob("assistant_tool_use", "toolu_m1b", t0)
	real.Seq = 1
	real.Attrs = map[string]any{"name": "Skill"}
	real.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"superpowers:verify"}`)}

	literal := ob("assistant_tool_use", "toolu_m1b", t0)
	literal.Seq = 2
	literal.Attrs = map[string]any{"name": "Skill"}

	g := NewGraph()
	g.ApplyAll([]model.Observation{real, literal})
	n := g.Nodes[model.ToolCallID(execID, "toolu_m1b")]
	assert.Equal(t, "superpowers:verify", n.Name)
}
