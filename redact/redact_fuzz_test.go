package redact_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/redact"
)

func FuzzRedact(f *testing.F) {
	f.Add([]byte(`{"token":"sk_live_ABCDEFGHIJKLMNOP0123"}`))
	f.Add([]byte("plain text AKIA" + "ABCDEFGHIJKLMNOP"))
	f.Add([]byte{0xff, 0xfe, 0x00})
	f.Add([]byte(`{"v":"9f86d081884c7d659a2feaa0c55ad015"}`))
	f.Add([]byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----"))
	f.Add([]byte(`9 export AWS_KEY=AKIAIOSFODNN7EXAMPLE`))
	f.Add([]byte(`{"password":"hunter2","note":"ya29.` + "abcdefghijklmnopqrstuvwxyz" + `"}`))
	f.Add([]byte(`{"password":"hunter2"} x`))
	f.Add([]byte(`{"a":1} tail wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`))
	f.Fuzz(func(t *testing.T, in []byte) {
		r1 := redact.Redact(in)
		r2 := redact.Redact(r1.Data)
		require.Equal(t, r1.Data, r2.Data)
	})
}

type survivalCase struct {
	family string
	secret string
}

var survivalCases = []survivalCase{
	{"aws-key", "AKIAIOSFODNN7EXAMPLE"},
	{"github-token", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef0123"},
	{"github-pat", "github_pat_ABCDEFGHIJKLMNOPQRSTUV0123456789abcdefgh"},
	{"openai-key", "sk-FakeSyntheticKey0123456789abcdef"},
	{"slack-token", "xoxb-1234" + "567890-FAKEFAKEFAKEFAKE"},
	{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlZmFrZQ"},
	{"pem-private-key", "-----BEGIN RSA PRIVATE KEY-----\nMIIFakeSyntheticKeyBody0123\n-----END RSA PRIVATE KEY-----"},
	{"google-api-key", "AIzaSyFakeSynthetic0123456789abcdefGHIJK"},
	{"stripe-key", "sk_live_FakeSynthetic0123456789"},
	{"sendgrid-key", "SG.FakeSynthetic0123.AbCdEfGhIjKlMnOpQrStUvWx"},
	{"high-entropy-hex", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
	{"bearer-token", "Bearer FakeSynthetic.Bearer0123456789"},
	{"connection-string", "postgres://svc:FakeSyntheticPw123@db.internal.example:5432/prod"},
	{"twilio-key", "SK01234567" + "89abcdef0123456789abcdef"},
	{"npm-token", "npm_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"},
	{"pypi-token", "pypi-FakeSynthetic0123456789"},
	{"gitlab-token", "glpat-FakeSynthetic0123456789"},
	{"google-oauth", "ya29.FakeSynthetic0123456789abcdef"},
}

func isWordly(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= 'a' && b <= 'z':
		return true
	default:
		return b == '_' || b == '-'
	}
}

const maxSurround = 4 << 10

func truncateSurround(s string) string {
	if len(s) > maxSurround {
		return s[:maxSurround]
	}
	return s
}

func spliceSecret(pre, secret, post string) string {
	pre = truncateSurround(pre)
	post = truncateSurround(post)
	var b strings.Builder
	b.WriteString(pre)
	if pre != "" && isWordly(pre[len(pre)-1]) && isWordly(secret[0]) {
		b.WriteByte('\n')
	}
	b.WriteString(secret)
	if post != "" && isWordly(secret[len(secret)-1]) && isWordly(post[0]) {
		b.WriteByte('\n')
	}
	b.WriteString(post)
	return b.String()
}

func FuzzRedactSecretSurvival(f *testing.F) {
	var seedChoice uint8
	for range survivalCases {
		f.Add("", "", seedChoice)
		f.Add("deploy log: ", " end of line", seedChoice)
		f.Add(`{"note":"`, `"}`, seedChoice)
		f.Add(`{"a":1} trailing `, "", seedChoice)
		f.Add(`0}`, "", seedChoice)
		f.Add(`{"a":1}}`, "", seedChoice)
		f.Add(`{"`, `":1}`, seedChoice)
		seedChoice++
	}
	f.Fuzz(func(t *testing.T, pre, post string, choice uint8) {
		c := survivalCases[int(choice)%len(survivalCases)]
		in := spliceSecret(pre, c.secret, post)
		res := redact.Redact([]byte(in))
		require.NotContains(t, string(res.Data), c.secret, "family %s survived redaction of input %q", c.family, in)
	})
}
