package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/realkarych/catacomb/ingest/codex"
)

var codexRolloutNameRe = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-(.+?)\.jsonl(\.zst)?$`)

func codexThreadIDFromFilename(name string) string {
	m := codexRolloutNameRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1]
}

func resolveCodexTranscripts(sessionsRoot, threadID string) (transcriptSet, error) {
	pattern := filepath.Join(sessionsRoot, "*", "*", "*", "rollout-*-"+threadID+".jsonl")
	mains, err := filepath.Glob(pattern)
	if err != nil {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: %w", err)
	}
	zst, _ := filepath.Glob(pattern + ".zst")
	mains = codexExactThreadMatches(append(mains, zst...), threadID)
	if len(mains) == 0 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: no transcript for session %s under %s", threadID, sessionsRoot)
	}
	if len(mains) > 1 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: ambiguous session %s: %d matches", threadID, len(mains))
	}
	subs, err := codexChildTranscripts(sessionsRoot, threadID)
	if err != nil {
		return transcriptSet{}, err
	}
	return transcriptSet{Main: mains[0], Subagents: subs}, nil
}

func codexExactThreadMatches(paths []string, threadID string) []string {
	var exact []string
	for _, p := range paths {
		if codexThreadIDFromFilename(filepath.Base(p)) == threadID {
			exact = append(exact, p)
		}
	}
	return exact
}

type codexRolloutRef struct {
	path     string
	threadID string
}

func codexChildTranscripts(root, threadID string) ([]string, error) {
	children := map[string][]codexRolloutRef{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || codexThreadIDFromFilename(d.Name()) == "" {
			return nil
		}
		id, ok := peekCodexIdentity(path)
		if !ok || id.parent == "" {
			return nil
		}
		children[id.parent] = append(children[id.parent], codexRolloutRef{path: path, threadID: id.threadID})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("resolve transcripts: %w", walkErr)
	}
	return collectCodexDescendants(children, threadID), nil
}

func collectCodexDescendants(children map[string][]codexRolloutRef, rootID string) []string {
	visited := map[string]struct{}{rootID: {}}
	queue := []string{rootID}
	var out []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, c := range children[id] {
			if _, seen := visited[c.threadID]; seen {
				continue
			}
			visited[c.threadID] = struct{}{}
			out = append(out, c.path)
			queue = append(queue, c.threadID)
		}
	}
	sort.Strings(out)
	return out
}

type codexIdentity struct {
	threadID string
	parent   string
}

type codexPeekThreadSpawn struct {
	ParentThreadID string `json:"parent_thread_id"`
}

type codexPeekSubagent struct {
	ThreadSpawn codexPeekThreadSpawn `json:"thread_spawn"`
}

type codexPeekSource struct {
	Subagent codexPeekSubagent `json:"subagent"`
}

type codexPeekPayload struct {
	SessionID      string          `json:"session_id"`
	ID             string          `json:"id"`
	ParentThreadID string          `json:"parent_thread_id"`
	Source         json.RawMessage `json:"source"`
}

type codexPeekLine struct {
	Type    string           `json:"type"`
	Payload codexPeekPayload `json:"payload"`
}

func peekCodexIdentity(path string) (codexIdentity, bool) {
	rc, err := codex.Open(path)
	if err != nil {
		return codexIdentity{}, false
	}
	defer func() { _ = rc.Close() }()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	if !sc.Scan() {
		return codexIdentity{}, false
	}
	var ln codexPeekLine
	if err := json.Unmarshal(sc.Bytes(), &ln); err != nil || ln.Type != "session_meta" {
		return codexIdentity{}, false
	}
	id := codexIdentity{threadID: ln.Payload.SessionID, parent: ln.Payload.ParentThreadID}
	if id.threadID == "" {
		id.threadID = ln.Payload.ID
	}
	if id.parent == "" {
		var src codexPeekSource
		if err := json.Unmarshal(ln.Payload.Source, &src); err == nil {
			id.parent = src.Subagent.ThreadSpawn.ParentThreadID
		}
	}
	return id, true
}
