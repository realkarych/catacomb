package model_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"single", "basket=checkout", map[string]string{"basket": "checkout"}},
		{"multi", "a=1,b=2", map[string]string{"a": "1", "b": "2"}},
		{"last write wins", "a=1,a=2", map[string]string{"a": "2"}},
		{"spaces trimmed", " a = 1 , b = 2 ", map[string]string{"a": "1", "b": "2"}},
		{"invalid key dropped", "A!=x,ok=1", map[string]string{"ok": "1"}},
		{"missing value dropped", "a,b=2", map[string]string{"b": "2"}},
		{"empty value kept", "a=,b=2", map[string]string{"a": "", "b": "2"}},
		{"value cap dropped", "a=" + strings.Repeat("x", 257) + ",b=2", map[string]string{"b": "2"}},
		{"value byte cap multibyte dropped", "a=" + strings.Repeat("я", 129) + ",b=2", map[string]string{"b": "2"}},
		{"key cap dropped", strings.Repeat("k", 65) + "=1,b=2", map[string]string{"b": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, model.ParseLabels(tt.in))
		})
	}
}

func TestParseLabelsPairCapKeepsTheFirstThirtyTwoKeysInInputOrder(t *testing.T) {
	var b strings.Builder
	want := map[string]string{}
	for i := range 40 {
		if i > 0 {
			b.WriteByte(',')
		}
		key := fmt.Sprintf("k%02d", i)
		fmt.Fprintf(&b, "%s=v%02d", key, i)
		if i < 32 {
			want[key] = fmt.Sprintf("v%02d", i)
		}
	}
	assert.Equal(t, want, model.ParseLabels(b.String()))
}

func TestParseLabelsPairCapUpdateExistingDropNew(t *testing.T) {
	var b strings.Builder
	for i := range 32 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "k%d=v", i)
	}
	b.WriteString(",k0=new,extra=x")
	got := model.ParseLabels(b.String())
	assert.Len(t, got, 32)
	assert.Equal(t, "new", got["k0"])
	_, ok := got["extra"]
	assert.False(t, ok)
}

func TestFormatLabelsSortsKeysRegardlessOfMapIterationOrder(t *testing.T) {
	in := map[string]string{}
	var want []string
	for i := range 16 {
		key := fmt.Sprintf("k%02d", i)
		in[key] = fmt.Sprintf("v%02d", i)
		want = append(want, key+"="+in[key])
	}
	assert.Equal(t, strings.Join(want, ","), model.FormatLabels(in))
	assert.Equal(t, "", model.FormatLabels(nil))
}

func TestFormatLabelsRoundTripsThroughParseLabels(t *testing.T) {
	in := map[string]string{"b": "2", "a": "1", "c.d": ""}
	assert.Equal(t, "a=1,b=2,c.d=", model.FormatLabels(in))
	assert.Equal(t, in, model.ParseLabels(model.FormatLabels(in)))
}

func TestMergeLabels(t *testing.T) {
	dst := map[string]string{"a": "1"}
	got := model.MergeLabels(dst, map[string]string{"a": "2", "b": "3"})
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, got)
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, dst)
	assert.Equal(t, map[string]string{"x": "1"}, model.MergeLabels(nil, map[string]string{"x": "1"}))
}
