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

func TestParseLabelsPairCap(t *testing.T) {
	var b strings.Builder
	for i := range 40 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(string(rune('a'+i%26)) + string(rune('0'+i/26)) + "=v")
	}
	assert.Len(t, model.ParseLabels(b.String()), 32)
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

func TestFormatLabelsCanonical(t *testing.T) {
	assert.Equal(t, "a=1,b=2", model.FormatLabels(map[string]string{"b": "2", "a": "1"}))
	assert.Equal(t, "", model.FormatLabels(nil))
}

func TestMergeLabels(t *testing.T) {
	dst := map[string]string{"a": "1"}
	got := model.MergeLabels(dst, map[string]string{"a": "2", "b": "3"})
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, got)
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, dst)
	assert.Equal(t, map[string]string{"x": "1"}, model.MergeLabels(nil, map[string]string{"x": "1"}))
}

func TestMatchLabels(t *testing.T) {
	labels := map[string]string{"basket": "checkout", "variant": "v2"}
	assert.True(t, model.MatchLabels(labels, map[string]string{"basket": "checkout"}))
	assert.True(t, model.MatchLabels(labels, map[string]string{}))
	assert.True(t, model.MatchLabels(labels, nil))
	assert.False(t, model.MatchLabels(labels, map[string]string{"basket": "other"}))
	assert.False(t, model.MatchLabels(labels, map[string]string{"missing": "x"}))
	assert.False(t, model.MatchLabels(nil, map[string]string{"a": "1"}))
}

func TestMatchLabelsEmptyValueVsMissingKey(t *testing.T) {
	assert.False(t, model.MatchLabels(nil, map[string]string{"a": ""}))
	assert.False(t, model.MatchLabels(map[string]string{"b": "1"}, map[string]string{"a": ""}))
	assert.True(t, model.MatchLabels(map[string]string{"a": ""}, map[string]string{"a": ""}))
}
