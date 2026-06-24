package redact

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"
)

type Finding struct {
	Path   string
	Reason string
}

type Result struct {
	Data     []byte
	Findings []Finding
	Redacted bool
}

var (
	reAWSKey = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)

	reGitHubToken = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)

	reGitHubPAT = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`)

	reOpenAIKey = regexp.MustCompile(`\bsk-[A-Za-z0-9-]{20,}\b`)

	reSlackToken = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9]([A-Za-z0-9-]{8,})\b`)

	reJWT = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\b`)

	rePEMPrivateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)

	reGoogleAPIKey = regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35,}\b`)

	reBearerToken = regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/=-]{10,}\b`)

	reConnectionString = regexp.MustCompile(`[a-z][a-z0-9+\-.]*://[^:@\s/]+:[^@\s]+@[^\s"'` + "`" + `]+`)

	reHexEntropy = regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`)

	reBase64Entropy = regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`)
)

type valueRule struct {
	re     *regexp.Regexp
	reason string
}

var valueRules = []valueRule{
	{reConnectionString, "connection-string"},
	{reAWSKey, "aws-key"},
	{reGitHubToken, "github-token"},
	{reGitHubPAT, "github-token"},
	{reOpenAIKey, "openai-key"},
	{reSlackToken, "slack-token"},
	{rePEMPrivateKey, "pem-private-key"},
	{reGoogleAPIKey, "google-api-key"},
	{reBearerToken, "bearer-token"},
	{reJWT, "jwt"},
	{reHexEntropy, "high-entropy"},
	{reBase64Entropy, "high-entropy"},
}

var sensitiveKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^password$`),
	regexp.MustCompile(`(?i)^passwd$`),
	regexp.MustCompile(`(?i)^secret$`),
	regexp.MustCompile(`(?i)^token$`),
	regexp.MustCompile(`(?i)^api[_-]?key$`),
	regexp.MustCompile(`(?i)^apikey$`),
	regexp.MustCompile(`(?i)^authorization$`),
	regexp.MustCompile(`(?i)^auth$`),
	regexp.MustCompile(`(?i)^access[_-]?token$`),
	regexp.MustCompile(`(?i)^refresh[_-]?token$`),
	regexp.MustCompile(`(?i)^client[_-]?secret$`),
	regexp.MustCompile(`(?i)^private[_-]?key$`),
	regexp.MustCompile(`(?i)^credentials?$`),
	regexp.MustCompile(`(?i)^session[_-]?key$`),
}

const redactedPlaceholder = "[REDACTED]"

func matchValueRule(s string) string {
	for _, rule := range valueRules {
		if rule.re.MatchString(s) {
			return rule.reason
		}
	}
	return ""
}

func Redact(raw []byte) Result {
	if len(raw) == 0 {
		return Result{Data: raw}
	}

	if !utf8.Valid(raw) {
		h := sha256.Sum256(raw)
		ref := fmt.Sprintf(`"‹binary:%d,%x›"`, len(raw), h[:8])
		return Result{
			Data:     []byte(ref),
			Findings: []Finding{{Path: "", Reason: "binary"}},
			Redacted: true,
		}
	}

	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return redactFreeText(raw)
	}

	var findings []Finding
	redacted := walkNode(node, "", &findings)
	if len(findings) == 0 {
		return Result{Data: raw}
	}

	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Path < findings[j].Path
	})

	out, _ := json.Marshal(redacted)

	return Result{
		Data:     out,
		Findings: findings,
		Redacted: true,
	}
}

func redactFreeText(raw []byte) Result {
	text := string(raw)
	var findings []Finding
	result := text
	for _, rule := range valueRules {
		if rule.re.MatchString(result) {
			result = rule.re.ReplaceAllString(result, redactedPlaceholder)
			findings = append(findings, Finding{Path: "", Reason: rule.reason})
		}
	}
	if len(findings) == 0 {
		return Result{Data: raw}
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Reason < findings[j].Reason
	})
	return Result{
		Data:     []byte(result),
		Findings: findings,
		Redacted: true,
	}
}

func walkNode(node any, path string, findings *[]Finding) any {
	switch v := node.(type) {
	case map[string]any:
		return walkObject(v, path, findings)
	case []any:
		return walkArray(v, path, findings)
	case string:
		return redactStringValue(v, path, findings)
	default:
		return node
	}
}

func walkObject(obj map[string]any, path string, findings *[]Finding) map[string]any {
	result := make(map[string]any, len(obj))
	for k, v := range obj {
		childPath := joinPath(path, k)
		if isSensitiveKey(k) {
			if sv, ok := v.(string); ok {
				reason := matchValueRule(sv)
				if reason == "" {
					reason = "sensitive-key"
				}
				result[k] = redactedPlaceholder
				*findings = append(*findings, Finding{Path: childPath, Reason: reason})
			} else {
				result[k] = walkNode(v, childPath, findings)
			}
		} else {
			result[k] = walkNode(v, childPath, findings)
		}
	}
	return result
}

func walkArray(arr []any, path string, findings *[]Finding) []any {
	result := make([]any, len(arr))
	for i, v := range arr {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		result[i] = walkNode(v, childPath, findings)
	}
	return result
}

func redactStringValue(s, path string, findings *[]Finding) string {
	for _, rule := range valueRules {
		if rule.re.MatchString(s) {
			*findings = append(*findings, Finding{Path: path, Reason: rule.reason})
			return redactedPlaceholder
		}
	}
	return s
}

func isSensitiveKey(k string) bool {
	for _, pat := range sensitiveKeyPatterns {
		if pat.MatchString(k) {
			return true
		}
	}
	return false
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
