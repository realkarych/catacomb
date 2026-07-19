package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type surroundingSample struct {
	secret            string
	delimsNotInSecret []string
}

var delimsSafeForEveryRule = []string{",", " ", "|", ";"}

var delimsSafeForRulesWithoutBase64Padding = []string{",", " ", "|", ";", "="}

var delimsTerminatingAConnectionString = []string{" ", "\t", `"`}

var samplesByRulePattern = map[string]surroundingSample{
	reConnectionString.String(): {
		"postgres://svc:FakeSyntheticPw123@db.internal.example:5432/prod",
		delimsTerminatingAConnectionString,
	},
	reAWSKey.String(): {
		"AKIAIOSFODNN7EXAMPLE",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reGitHubToken.String(): {
		"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef0123",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reGitHubPAT.String(): {
		"github_pat_ABCDEFGHIJKLMNOPQRSTUV0123456789abcdefgh",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reOpenAIKey.String(): {
		"sk-FakeSyntheticKey0123456789abcdef",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reSlackToken.String(): {
		"xoxb-1234" + "567890-FAKEFAKEFAKEFAKE",
		delimsSafeForRulesWithoutBase64Padding,
	},
	rePEMMarker.String(): {
		"-----BEGIN RSA PRIVATE KEY-----",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reGoogleAPIKey.String(): {
		"AIzaSyFakeSynthetic0123456789abcdefGHIJK",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reBearerToken.String(): {
		"Bearer FakeSynthetic.Bearer0123456789",
		delimsSafeForEveryRule,
	},
	reJWT.String(): {
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlZmFrZQ",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reStripeKey.String(): {
		"sk_live_FakeSynthetic0123456789",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reSendGrid.String(): {
		"SG.FakeSynthetic0123.AbCdEfGhIjKlMnOpQrStUvWx",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reTwilioKey.String(): {
		"SK01234567" + "89abcdef0123456789abcdef",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reNPMToken.String(): {
		"npm_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		delimsSafeForRulesWithoutBase64Padding,
	},
	rePyPIToken.String(): {
		"pypi-FakeSynthetic0123456789",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reGitLabPAT.String(): {
		"glpat-FakeSynthetic0123456789",
		delimsSafeForRulesWithoutBase64Padding,
	},
	reGoogleOAuth.String(): {
		"ya29.FakeSynthetic0123456789abcdef",
		delimsSafeForRulesWithoutBase64Padding,
	},
	entropyRules[0].re.String(): {
		"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		delimsSafeForRulesWithoutBase64Padding,
	},
	entropyRules[1].re.String(): {
		"aB3xQ7zK9mR2tV5wY8cE1gH4jL6nP0sU",
		delimsSafeForEveryRule,
	},
	entropyRules[2].re.String(): {
		"aB3xQ7zK9mR2tV5wY8c-E1gH4jL6nP0sU7",
		delimsSafeForEveryRule,
	},
	entropyRules[3].re.String(): {
		"aB3xQ7zK9mR2tV5wY8c/E1gH4jL6nP0sU7dF2iM5oT8qW4",
		delimsSafeForEveryRule,
	},
}

func allRulePatterns() []string {
	patterns := make([]string, 0, len(valueRules)+len(entropyRules)+1)
	for _, rule := range valueRules {
		patterns = append(patterns, rule.re.String())
	}
	for _, rule := range entropyRules {
		patterns = append(patterns, rule.re.String())
	}
	return patterns
}

func TestSurroundingSamples_CoverEveryValueAndEntropyRule(t *testing.T) {
	patterns := allRulePatterns()
	seen := make(map[string]bool, len(patterns))
	for _, pattern := range patterns {
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		_, ok := samplesByRulePattern[pattern]
		assert.True(t, ok, "rule %s has no surrounding-text sample; add one so the delimiter guard covers it", pattern)
	}
	assert.Len(t, samplesByRulePattern, len(seen), "samplesByRulePattern has entries that match no live rule")
}

func TestSurroundingSamples_AreRealSecretsForTheirOwnRule(t *testing.T) {
	for _, rule := range valueRules {
		sample, ok := samplesByRulePattern[rule.re.String()]
		require.True(t, ok)
		assert.True(t, rule.re.MatchString(sample.secret), "sample %q is not matched by rule %s", sample.secret, rule.reason)
	}
	for _, rule := range entropyRules {
		sample, ok := samplesByRulePattern[rule.re.String()]
		require.True(t, ok)
		matches := rule.re.FindAllString(sample.secret, -1)
		require.NotEmpty(t, matches, "sample %q is not matched by entropy rule %s", sample.secret, rule.re)
		var qualified bool
		for _, m := range matches {
			if shannonEntropy(m) >= rule.minBits {
				qualified = true
			}
		}
		assert.True(t, qualified, "sample %q never reaches %v bits for rule %s", sample.secret, rule.minBits, rule.re)
	}
}

func TestValueRules_CaptureGroupsExistOnlyToReemitALeadingDelimiter(t *testing.T) {
	for _, rule := range valueRules {
		if rule.reemittedLeadingGroup == "" {
			assert.Zero(t, rule.re.NumSubexp(),
				"rule %s captures a group it never re-emits; make the group non-capturing so no future replacement can echo secret material", rule.reason)
			continue
		}
		assert.Equal(t, firstGroupIsALeadingDelimiter, rule.reemittedLeadingGroup, "rule %s re-emits an unexpected group", rule.reason)
		assert.Equal(t, 1, rule.re.NumSubexp(), "rule %s must expose exactly one group, the leading delimiter", rule.reason)
	}
}

func TestRedact_NoRuleConsumesTheDelimiterPrecedingTheSecret(t *testing.T) {
	for pattern, sample := range samplesByRulePattern {
		for _, delim := range delimsSafeForRulesWithoutBase64Padding {
			input := "PRE" + delim + sample.secret
			out := string(Redact([]byte(input)).Data)
			assert.NotContains(t, out, sample.secret, "rule %s leaked its secret", pattern)
			assert.Contains(t, out, "PRE"+delim, "rule %s deleted the %q preceding the secret: %q", pattern, delim, out)
		}
	}
}

func TestRedact_EveryRulePreservesTextSurroundingTheSecret(t *testing.T) {
	for pattern, sample := range samplesByRulePattern {
		for _, delim := range sample.delimsNotInSecret {
			input := "PRE" + delim + sample.secret + delim + "POST"
			out := string(Redact([]byte(input)).Data)
			assert.NotContains(t, out, sample.secret, "rule %s leaked its secret", pattern)
			assert.Contains(t, out, "PRE"+delim, "rule %s deleted the leading %q: %q", pattern, delim, out)
			assert.Contains(t, out, delim+"POST", "rule %s deleted the trailing %q: %q", pattern, delim, out)
		}
	}
}
