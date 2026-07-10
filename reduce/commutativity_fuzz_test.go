package reduce

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func commutativityCorpus() []model.Observation {
	t0 := time.Unix(100, 0).UTC()
	return []model.Observation{
		unknownKindObs("e1", "s1", "checkpoint", 1),
		hookTurn("e1", "s1", "m1", 5, 2, t0, 2),
		jsonlTurnNoTokens("e1", "s1", "m1", t0.Add(time.Second), 3),
		toolObs("e1", "s1", "t1", "Bash", "running", 4),
		toolObs("e1", "s1", "t1", "Bash", string(model.StatusOK), 6),
		jsonlTool("e1", "s1", "t2", 7),
		toolObs("e1", "s1", "t3", "mcp__fs__read", "running", 8),
		toolObs("e1", "s1", "t3", "mcp__fs__read", string(model.StatusOK), 9),
		unknownKindObs("e1", "s1", "diagnostic", 10),
		toolObs("e1", "s1", "t4", "Read", string(model.StatusOK), 11),
		unknownKindObs("e1", "s1", "checkpoint", 12),
		unknownKindObs("e1", "s1", "diagnostic", 13),
	}
}

func canonicalCommutativityGraph() string {
	g := NewGraph()
	g.ApplyAll(commutativityCorpus())
	return canonGraph(g)
}

func shuffledBySeed(seed uint64) []model.Observation {
	obs := commutativityCorpus()
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	r.Shuffle(len(obs), func(i, j int) { obs[i], obs[j] = obs[j], obs[i] })
	return obs
}

func FuzzReductionCommutativity(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(42))
	f.Add(uint64(6364136223846793005))
	f.Add(^uint64(0))
	want := canonicalCommutativityGraph()
	f.Fuzz(func(t *testing.T, seed uint64) {
		g := NewGraph()
		g.ApplyAll(shuffledBySeed(seed))
		assert.Equal(t, want, canonGraph(g), "reduction diverged for shuffle seed %d", seed)
	})
}
