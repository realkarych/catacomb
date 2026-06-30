package reduce

import (
	"encoding/json"
	"strings"

	"github.com/realkarych/catacomb/model"
)

func isSkill(name string) bool {
	return name == "Skill" || name == "SlashCommand"
}

type skillToolInput struct {
	Skill   string `json:"skill"`
	Command string `json:"command"`
}

func extractSkillName(o model.Observation) string {
	if o.Payload == nil || len(o.Payload.Input) == 0 {
		return ""
	}
	var in skillToolInput
	if err := json.Unmarshal(o.Payload.Input, &in); err != nil {
		return ""
	}
	if in.Skill != "" {
		return in.Skill
	}
	return cleanCommand(in.Command)
}

func cleanCommand(cmd string) string {
	cmd = strings.TrimPrefix(strings.TrimSpace(cmd), "/")
	if f := strings.Fields(cmd); len(f) > 0 {
		return f[0]
	}
	return ""
}

func toolDisplayName(o model.Observation, name string) (string, bool) {
	if isSkill(name) {
		if sn := extractSkillName(o); sn != "" {
			return sn, true
		}
	}
	return name, false
}
