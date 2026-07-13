package jsonl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realkarych/catacomb/model"
)

func FuzzParse(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.jsonl"))
	if err != nil {
		f.Fatal(err)
	}
	for _, p := range paths {
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			f.Fatal(rerr)
		}
		f.Add(data)
	}
	f.Add([]byte("{not json}\n"))
	f.Add([]byte(`{"type":"user","message":123}`))
	f.Add([]byte(`{"type":"user","message":{"role":"user","content":5}}`))
	f.Add([]byte(`{"type":"assistant","timestamp":"not-a-time","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}]}}`))
	f.Add([]byte("{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"h\xffi\"}}\n"))
	f.Add([]byte("{\"type\":\"user\",\"uuid\":\"a\x00b\"}\n"))
	f.Add([]byte(strings.Repeat("[", 128) + strings.Repeat("]", 128)))
	f.Add([]byte("\n\n  \n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var seq uint64
		next := func() uint64 {
			s := seq
			seq++
			return s
		}
		obs, _, perr := Parse(bytes.NewReader(data), "exec-fuzz", next, func(ts time.Time) time.Time { return ts })
		if perr != nil {
			if obs != nil {
				t.Fatalf("Parse returned %d observations alongside error %v", len(obs), perr)
			}
			return
		}
		for i, o := range obs {
			if o.ObsID == "" {
				t.Fatalf("observation %d missing ObsID", i)
			}
			if o.ExecutionID != "exec-fuzz" {
				t.Fatalf("observation %d has execution id %q", i, o.ExecutionID)
			}
			if o.Source != model.SourceJSONL {
				t.Fatalf("observation %d has source %q", i, o.Source)
			}
			if o.Seq != uint64(i) {
				t.Fatalf("observation %d has seq %d", i, o.Seq)
			}
		}
	})
}
