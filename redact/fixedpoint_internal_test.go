package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func allReasons() []string {
	seen := map[string]bool{"sensitive-key": true, "binary": true}
	for _, rule := range valueRules {
		seen[rule.reason] = true
	}
	for _, rule := range entropyRules {
		seen[rule.reason] = true
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	return out
}

func TestPlaceholdersNeverRematchRulePack(t *testing.T) {
	for _, reason := range allReasons() {
		r := Redact([]byte(placeholder(reason)))
		assert.False(t, r.Redacted, "placeholder for %q rematches the rule pack", reason)
		assert.Equal(t, placeholder(reason), string(r.Data))
	}
}

func TestTypedRefsNeverRematchRulePack(t *testing.T) {
	for _, ref := range []string{
		`"‹binary:1048576,0123456789abcdef›"`,
		`"‹ref:1048576,0123456789abcdef›"`,
		"note: ‹binary:1048576,0123456789abcdef› and ‹ref:42,fedcba9876543210›",
	} {
		r := Redact([]byte(ref))
		assert.False(t, r.Redacted, ref)
	}
}
