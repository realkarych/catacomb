package main

import (
	"runtime"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
)

func benchEnvStamps(runs []model.Run, sessionID string, ws *bench.Workspace) *evidence.EnvStamps {
	env := baseEnvStamps(runs, sessionID, ws)
	env.AgentRuntime = drift.RuntimeClaudeCode
	env.AgentVersion = env.ClaudeCodeVersion
	return env
}

func codexEnvStamps(runs []model.Run, sessionID, agentVersion string) *evidence.EnvStamps {
	env := baseEnvStamps(runs, sessionID, nil)
	env.AgentRuntime = drift.RuntimeCodex
	env.AgentVersion = agentVersion
	return env
}

func importEnvStamps(rt string, runs []model.Run, sessionID string, obs []model.Observation) *evidence.EnvStamps {
	if rt == drift.RuntimeCodex {
		return codexEnvStamps(runs, sessionID, maxObservedVersionFor(rt, obs))
	}
	return benchEnvStamps(runs, sessionID, nil)
}

func baseEnvStamps(runs []model.Run, sessionID string, ws *bench.Workspace) *evidence.EnvStamps {
	env := &evidence.EnvStamps{
		CatacombVersion: Version,
		Resources:       evidence.Resources{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()},
	}
	if ws != nil {
		env.Workspace = &evidence.WorkspaceStamp{Rev: ws.Rev, PatchSHA256: ws.PatchSHA256}
	}
	for _, r := range runs {
		if r.ID != sessionID {
			continue
		}
		env.ModelID = r.ModelID
		if r.Repro != nil {
			env.ClaudeCodeVersion = r.Repro.ClaudeCodeVersion
		}
	}
	return env
}
