package codex

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			f.Add([]byte(sc.Text()))
		}
		if serr := sc.Err(); serr != nil {
			f.Fatal(serr)
		}
	}
	f.Add([]byte(""))
	f.Add([]byte("{not json}\n"))
	f.Add([]byte(`{"type":"session_meta","payload":{"id":`))
	f.Add([]byte(`{"type":"session_meta","payload":123}`))
	f.Add([]byte(`{"type":"turn_context","payload":"x"}`))
	f.Add([]byte(`{"type":"response_item","payload":[]}`))
	f.Add([]byte(`{"type":"event_msg","payload":"x"}`))
	f.Add([]byte(`{"timestamp":"not-a-time","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`))
	f.Add([]byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"h\xffi\"}}\n"))
	f.Add([]byte(strings.Repeat("[", 128) + strings.Repeat("]", 128)))
	f.Add([]byte("\n\n  \n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		seq := uint64(1)
		next := func() uint64 {
			s := seq
			seq++
			return s
		}
		obs, _, perr := Parse(bytes.NewReader(data), "run-fuzz", "exec-fuzz", next, identityObservedAt)
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
			if o.Seq != uint64(i+1) {
				t.Fatalf("observation %d has zero or out-of-order seq %d", i, o.Seq)
			}
		}
	})
}
