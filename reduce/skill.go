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
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd
}

func toolDisplayName(o model.Observation, name string) string {
	if isSkill(name) {
		if sn := extractSkillName(o); sn != "" {
			return sn
		}
	}
	return name
}
