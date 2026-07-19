package reduce

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func fillEveryFieldNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Interface:
		v.Set(reflect.ValueOf("x"))
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillEveryFieldNonZero(v.Elem())
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		fillEveryFieldNonZero(k)
		e := reflect.New(v.Type().Elem()).Elem()
		fillEveryFieldNonZero(e)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Slice:
		if v.Type() == reflect.TypeOf(json.RawMessage(nil)) {
			v.Set(reflect.ValueOf(json.RawMessage(`"x"`)))
			return
		}
		e := reflect.New(v.Type().Elem()).Elem()
		fillEveryFieldNonZero(e)
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 1), e))
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Unix(1, 0).UTC()))
			return
		}
		for i := range v.NumField() {
			fillEveryFieldNonZero(v.Field(i))
		}
	}
}

func jsonFieldNames(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			out = append(out, tag)
		}
	}
	return out
}

func fullyPopulated[T any]() *T {
	var v T
	fillEveryFieldNonZero(reflect.ValueOf(&v).Elem())
	return &v
}

func TestCanonGraphComparesEveryNodeEdgeAndRunField(t *testing.T) {
	n := fullyPopulated[model.Node]()
	e := fullyPopulated[model.Edge]()
	r := fullyPopulated[model.Run]()

	g := NewGraph()
	g.Nodes[n.ID] = n
	g.Edges[e.ID] = e
	g.Runs[r.ID] = r
	canon := canonGraph(g)

	for _, tc := range []struct {
		kind   string
		fields []string
	}{
		{"node", jsonFieldNames(reflect.TypeOf(model.Node{}))},
		{"edge", jsonFieldNames(reflect.TypeOf(model.Edge{}))},
		{"run", jsonFieldNames(reflect.TypeOf(model.Run{}))},
	} {
		require.NotEmpty(t, tc.fields)
		for _, f := range tc.fields {
			assert.Contains(t, canon, `"`+f+`"`,
				"%s field %q is excluded from the canonical comparison, so a commutativity bug in it would be invisible", tc.kind, f)
		}
	}
}

func TestCanonGraphDetectsADifferenceInEveryNodeField(t *testing.T) {
	base := fullyPopulated[model.Node]()
	baseGraph := NewGraph()
	baseGraph.Nodes[base.ID] = base
	want := canonGraph(baseGraph)

	rt := reflect.TypeOf(model.Node{})
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		if name == "ID" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			mutated := fullyPopulated[model.Node]()
			reflect.ValueOf(mutated).Elem().Field(i).Set(reflect.Zero(rt.Field(i).Type))
			g := NewGraph()
			g.Nodes[mutated.ID] = mutated
			assert.NotEqual(t, want, canonGraph(g),
				"canonGraph cannot see a change to Node.%s", name)
		})
	}
}
