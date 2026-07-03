package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
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
	reAWSKey = regexp.MustCompile(`(?:AKIA|ASIA|AGPA|AROA|AIDA|ANPA|ANVA|AIPA|ABIA|ACCA)[0-9A-Z]{16}`)

	reGitHubToken = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)

	reGitHubPAT = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`)

	reOpenAIKey = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])sk-[A-Za-z0-9_-]{20,}`)

	reSlackToken = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9]([A-Za-z0-9-]{8,})\b`)

	reJWT = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\b`)

	rePEMMarker = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)

	rePEMBlock = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)

	reGoogleAPIKey = regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35,}\b`)

	reBearerToken = regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/=-]{10,}\b`)

	reConnectionString = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+\-.]*://(?:[^:@\s/]+:[^@\s]+@|:[^@\s]+@)[^\s"'` + "`" + `]+`)

	reHexEntropy = regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`)

	reBase64Entropy = regexp.MustCompile(`\b[A-Za-z0-9+]{40,}={0,2}\b`)
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
	{rePEMMarker, "pem-private-key"},
	{reGoogleAPIKey, "google-api-key"},
	{reBearerToken, "bearer-token"},
	{reJWT, "jwt"},
	{reHexEntropy, "high-entropy"},
	{reBase64Entropy, "high-entropy"},
}

var sensitiveKeyTokens = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"apikey",
	"api_key",
	"auth",
	"credential",
	"private_key",
	"privatekey",
	"sessionkey",
	"session_key",
}

func placeholder(reason string) string {
	return "‹redacted:" + reason + "›"
}

func HasMarker(data []byte) bool {
	s := string(data)
	return strings.Contains(s, "‹redacted:") || strings.Contains(s, "‹binary:") || strings.Contains(s, "‹ref:")
}

var knownPlaceholders = func() map[string]bool {
	m := map[string]bool{
		placeholder("sensitive-key"): true,
		placeholder("binary"):        true,
	}
	for _, rule := range valueRules {
		m[placeholder(rule.reason)] = true
	}
	return m
}()

func isKnownPlaceholder(s string) bool {
	return knownPlaceholders[s]
}

const typedRefCorePattern = `‹(?:ref|binary):\d+,[0-9a-f]+›`

var reTypedRefValue = regexp.MustCompile(`^` + typedRefCorePattern + `$`)

func isTypedRefValue(s string) bool {
	return reTypedRefValue.MatchString(s)
}

func matchValueRule(s string) string {
	for _, rule := range valueRules {
		if rule.re.MatchString(s) {
			return rule.reason
		}
	}
	return ""
}

func normalizeKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isSensitiveKey(k string) bool {
	norm := normalizeKey(k)
	for _, tok := range sensitiveKeyTokens {
		normTok := normalizeKey(tok)
		if strings.Contains(norm, normTok) {
			return true
		}
	}
	return false
}

const maxRedactPasses = 8

func Redact(raw []byte) Result {
	cur := raw
	var findings []Finding
	redacted := false
	for i := 0; i < maxRedactPasses; i++ {
		pass := redactOnce(cur)
		findings = mergeFindings(findings, pass.Findings)
		redacted = redacted || pass.Redacted
		if bytes.Equal(pass.Data, cur) {
			break
		}
		cur = pass.Data
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Path < findings[j].Path
	})
	return Result{Data: cur, Findings: findings, Redacted: redacted}
}

func mergeFindings(dst, src []Finding) []Finding {
	for _, f := range src {
		if !containsFinding(dst, f) {
			dst = append(dst, f)
		}
	}
	return dst
}

func containsFinding(fs []Finding, target Finding) bool {
	for _, f := range fs {
		if f == target {
			return true
		}
	}
	return false
}

func redactOnce(raw []byte) Result {
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

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var node any
	if err := dec.Decode(&node); err != nil {
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

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(redacted)
	out := bytes.TrimRight(buf.Bytes(), "\n")

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

	if rePEMBlock.MatchString(result) {
		result = rePEMBlock.ReplaceAllString(result, placeholder("pem-private-key"))
		findings = append(findings, Finding{Path: "", Reason: "pem-private-key"})
	} else if rePEMMarker.MatchString(result) {
		result = rePEMMarker.ReplaceAllString(result, placeholder("pem-private-key"))
		findings = append(findings, Finding{Path: "", Reason: "pem-private-key"})
	}

	for _, rule := range valueRules {
		if rule.re == rePEMMarker {
			continue
		}
		if rule.re.MatchString(result) {
			result = rule.re.ReplaceAllStringFunc(result, func(m string) string {
				return placeholder(rule.reason)
			})
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
				if isKnownPlaceholder(sv) || isTypedRefValue(sv) {
					result[k] = sv
				} else {
					reason := matchValueRule(sv)
					if reason == "" {
						reason = "sensitive-key"
					}
					result[k] = placeholder(reason)
					*findings = append(*findings, Finding{Path: childPath, Reason: reason})
				}
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

func redactStringValue(s, path string, findings *[]Finding) any {
	for _, rule := range valueRules {
		if rule.re.MatchString(s) {
			*findings = append(*findings, Finding{Path: path, Reason: rule.reason})
			return placeholder(rule.reason)
		}
	}
	return s
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
