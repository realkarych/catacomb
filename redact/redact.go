package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
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

	reStripeKey = regexp.MustCompile(`\b[rsp]k_(?:live|test)_[0-9A-Za-z]{16,}\b`)

	reSendGrid = regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\b`)

	reTwilioKey = regexp.MustCompile(`\bSK[0-9a-fA-F]{32}\b`)

	reNPMToken = regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)

	rePyPIToken = regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{16,}\b`)

	reGitLabPAT = regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)

	reGoogleOAuth = regexp.MustCompile(`\bya29\.[A-Za-z0-9._-]{20,}\b`)
)

func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

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
	{reStripeKey, "stripe-key"},
	{reSendGrid, "sendgrid-key"},
	{reTwilioKey, "twilio-key"},
	{reNPMToken, "npm-token"},
	{rePyPIToken, "pypi-token"},
	{reGitLabPAT, "gitlab-token"},
	{reGoogleOAuth, "google-oauth"},
}

type entropyRule struct {
	re      *regexp.Regexp
	reason  string
	minBits float64
}

var entropyRules = []entropyRule{
	{regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`), "high-entropy", 3.2},
	{regexp.MustCompile(`\b[A-Za-z0-9+]{32,}={0,2}\b`), "high-entropy", 4.0},
	{regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}={0,2}\b`), "high-entropy", 4.3},
	{regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`), "high-entropy", 4.4},
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

var knownPlaceholders = func() map[string]bool {
	m := map[string]bool{
		placeholder("sensitive-key"): true,
		placeholder("binary"):        true,
	}
	for _, rule := range valueRules {
		m[placeholder(rule.reason)] = true
	}
	for _, rule := range entropyRules {
		m[placeholder(rule.reason)] = true
	}
	return m
}()

func isKnownPlaceholder(s string) bool {
	return knownPlaceholders[s]
}

const typedRefCorePattern = `‹(?:ref|binary):\d+,[0-9a-f]{16}›`

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
	for _, rule := range entropyRules {
		for _, m := range rule.re.FindAllString(s, -1) {
			if shannonEntropy(m) >= rule.minBits {
				return rule.reason
			}
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
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Reason < findings[j].Reason
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
	if off := dec.InputOffset(); hasTrailingContent(raw, off) {
		switch node.(type) {
		case map[string]any, []any, string:
			return redactValueWithTail(node, raw, off)
		}
		return redactFreeText(raw)
	}

	var findings []Finding
	redacted := walkNode(node, "", &findings)
	if len(findings) == 0 {
		return Result{Data: raw}
	}

	sortFindings(findings)

	return Result{
		Data:     marshalJSON(redacted),
		Findings: findings,
		Redacted: true,
	}
}

func hasTrailingContent(raw []byte, off int64) bool {
	return len(bytes.TrimLeft(raw[off:], " \t\r\n")) > 0
}

func redactValueWithTail(node any, raw []byte, off int64) Result {
	var findings []Finding
	redacted := walkNode(node, "", &findings)
	head := raw[:off]
	if len(findings) > 0 {
		head = marshalJSON(redacted)
	}
	tail := redactFreeText(raw[off:])
	findings = mergeFindings(findings, tail.Findings)
	if len(findings) == 0 {
		return Result{Data: raw}
	}
	sortFindings(findings)
	return Result{
		Data:     slices.Concat(head, tail.Data),
		Findings: findings,
		Redacted: true,
	}
}

func marshalJSON(node any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(node)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Reason < findings[j].Reason
	})
}

func redactFreeText(raw []byte) Result {
	result, reasons := replaceSecretSpans(string(raw))
	if len(reasons) == 0 {
		return Result{Data: raw}
	}
	findings := make([]Finding, 0, len(reasons))
	for _, reason := range reasons {
		findings = append(findings, Finding{Path: "", Reason: reason})
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

func replaceSecretSpans(text string) (string, []string) {
	result := text
	var reasons []string

	if rePEMBlock.MatchString(result) {
		result = rePEMBlock.ReplaceAllString(result, placeholder("pem-private-key"))
		reasons = append(reasons, "pem-private-key")
	} else if rePEMMarker.MatchString(result) {
		result = rePEMMarker.ReplaceAllString(result, placeholder("pem-private-key"))
		reasons = append(reasons, "pem-private-key")
	}

	for _, rule := range valueRules {
		if rule.re == rePEMMarker {
			continue
		}
		if rule.re.MatchString(result) {
			result = rule.re.ReplaceAllStringFunc(result, func(string) string {
				return placeholder(rule.reason)
			})
			reasons = append(reasons, rule.reason)
		}
	}
	for _, rule := range entropyRules {
		replaced := false
		result = rule.re.ReplaceAllStringFunc(result, func(m string) string {
			if shannonEntropy(m) < rule.minBits {
				return m
			}
			replaced = true
			return placeholder(rule.reason)
		})
		if replaced {
			reasons = append(reasons, rule.reason)
		}
	}
	return result, reasons
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
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		v := obj[k]
		rk, keyReasons := replaceSecretSpans(k)
		childPath := joinPath(path, rk)
		for _, reason := range keyReasons {
			*findings = append(*findings, Finding{Path: childPath, Reason: reason})
		}
		if isSensitiveKey(k) {
			if sv, ok := v.(string); ok {
				if isKnownPlaceholder(sv) || isTypedRefValue(sv) {
					result[rk] = sv
				} else {
					reason := matchValueRule(sv)
					if reason == "" {
						reason = "sensitive-key"
					}
					result[rk] = placeholder(reason)
					*findings = append(*findings, Finding{Path: childPath, Reason: reason})
				}
			} else {
				result[rk] = walkNode(v, childPath, findings)
			}
		} else {
			result[rk] = walkNode(v, childPath, findings)
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
	result, reasons := replaceSecretSpans(s)
	if len(reasons) == 0 {
		return s
	}
	for _, reason := range reasons {
		*findings = append(*findings, Finding{Path: path, Reason: reason})
	}
	return result
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
