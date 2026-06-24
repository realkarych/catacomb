package redact_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/redact"
)

func findingPaths(findings []redact.Finding) []string {
	paths := make([]string, len(findings))
	for i, f := range findings {
		paths[i] = f.Path
	}
	return paths
}

func findingReasons(findings []redact.Finding) []string {
	reasons := make([]string, len(findings))
	for i, f := range findings {
		reasons[i] = f.Reason
	}
	return reasons
}

func containsSecret(data []byte, secret string) bool {
	return strings.Contains(string(data), secret)
}

func TestRedact_Empty(t *testing.T) {
	result := redact.Redact(nil)
	assert.Nil(t, result.Data)
	assert.Empty(t, result.Findings)
	assert.False(t, result.Redacted)

	result2 := redact.Redact([]byte{})
	assert.Empty(t, result2.Data)
	assert.Empty(t, result2.Findings)
	assert.False(t, result2.Redacted)
}

func TestRedact_Clean_NoSecrets(t *testing.T) {
	input := []byte(`{"name":"Alice","age":30,"city":"New York"}`)
	result := redact.Redact(input)
	assert.Empty(t, result.Findings)
	assert.False(t, result.Redacted)
	var out map[string]any
	require.NoError(t, json.Unmarshal(result.Data, &out))
	assert.Equal(t, "Alice", out["name"])
}

func TestRedact_Deterministic(t *testing.T) {
	input := []byte(`{"password":"supersecret","name":"Alice","token":"ghp_abcdefghijklmnopqrstuvwxyz1234567890"}`)
	r1 := redact.Redact(input)
	r2 := redact.Redact(input)
	assert.Equal(t, r1.Data, r2.Data)
	assert.Equal(t, r1.Findings, r2.Findings)
	assert.Equal(t, r1.Redacted, r2.Redacted)
}

func TestRedact_Binary_NonUTF8(t *testing.T) {
	input := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xde, 0xad, 0xbe, 0xef}
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "binary", result.Findings[0].Reason)
	assert.Contains(t, string(result.Data), "binary")
}

func TestRedact_ValueScan_AWS_Key(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		input := []byte(`{"key":"AKIAIOSFODNN7EXAMPLE"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "AKIAIOSFODNN7EXAMPLE"))
		reasons := findingReasons(result.Findings)
		assert.Contains(t, reasons, "aws-key")
	})
	t.Run("near-miss", func(t *testing.T) {
		input := []byte(`{"key":"BKIAIOSFODNN7EXAMPLE"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("near-miss-short", func(t *testing.T) {
		input := []byte(`{"key":"AKIA123"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_GitHub_Token(t *testing.T) {
	t.Run("ghp positive", func(t *testing.T) {
		token := "ghp_" + strings.Repeat("a", 36)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, token))
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
	t.Run("gho positive", func(t *testing.T) {
		token := "gho_" + strings.Repeat("b", 36)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("ghs positive", func(t *testing.T) {
		token := "ghs_" + strings.Repeat("c", 36)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("ghu positive", func(t *testing.T) {
		token := "ghu_" + strings.Repeat("d", 36)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("gpr positive", func(t *testing.T) {
		token := "gpr_" + strings.Repeat("e", 36)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"value":"ghp_short"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("github_pat positive", func(t *testing.T) {
		token := "github_pat_" + strings.Repeat("f", 40)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
}

func TestRedact_ValueScan_OpenAI_Key(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		key := "sk-" + strings.Repeat("A", 20)
		input := []byte(`{"api_key":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, key))
		assert.Contains(t, findingReasons(result.Findings), "openai-key")
	})
	t.Run("sk-ant positive", func(t *testing.T) {
		key := "sk-ant-" + strings.Repeat("B", 20)
		input := []byte(`{"key":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"key":"sk-short"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_Slack_Token(t *testing.T) {
	t.Run("xoxb positive", func(t *testing.T) {
		token := "xoxb-" + strings.Repeat("1", 10)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "slack-token")
	})
	t.Run("xoxa positive", func(t *testing.T) {
		token := "xoxa-" + strings.Repeat("2", 10)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("xoxp positive", func(t *testing.T) {
		token := "xoxp-" + strings.Repeat("3", 10)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("xoxr positive", func(t *testing.T) {
		token := "xoxr-" + strings.Repeat("4", 10)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("xoxs positive", func(t *testing.T) {
		token := "xoxs-" + strings.Repeat("5", 10)
		input := []byte(`{"token":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"value":"xoxb-abc"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("near-miss wrong prefix", func(t *testing.T) {
		input := []byte(`{"value":"xoxx-1234567890"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_JWT(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
		input := []byte(`{"auth":"` + jwt + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, jwt))
		assert.Contains(t, findingReasons(result.Findings), "jwt")
	})
	t.Run("near-miss missing third part", func(t *testing.T) {
		input := []byte(`{"value":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_PEM_Block(t *testing.T) {
	t.Run("positive RSA", func(t *testing.T) {
		pem := "-----BEGIN RSA PRIVATE KEY-----"
		input := []byte(`{"key":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, pem))
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
	t.Run("positive EC", func(t *testing.T) {
		pem := "-----BEGIN EC PRIVATE KEY-----"
		input := []byte(`{"key":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("positive generic PRIVATE KEY", func(t *testing.T) {
		pem := "-----BEGIN PRIVATE KEY-----"
		input := []byte(`{"key":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss public key", func(t *testing.T) {
		input := []byte(`{"key":"-----BEGIN PUBLIC KEY-----"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_Google_API_Key(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		key := "AIza" + strings.Repeat("B", 35)
		input := []byte(`{"key":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "google-api-key")
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"key":"AIzaShort"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_Bearer_Token(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		input := []byte(`{"header":"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.XXXX"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "bearer-token")
	})
	t.Run("near-miss no token value", func(t *testing.T) {
		input := []byte(`{"header":"Bearer "}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_ConnectionString(t *testing.T) {
	t.Run("postgres positive", func(t *testing.T) {
		connstr := "postgres://user:secretpass@localhost:5432/db"
		input := []byte(`{"dsn":"` + connstr + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, connstr))
		assert.Contains(t, findingReasons(result.Findings), "connection-string")
	})
	t.Run("mysql positive", func(t *testing.T) {
		connstr := "mysql://admin:pass123@db.example.com/mydb"
		input := []byte(`{"url":"` + connstr + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss no password", func(t *testing.T) {
		input := []byte(`{"url":"postgres://localhost:5432/db"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_HighEntropy(t *testing.T) {
	t.Run("long hex positive", func(t *testing.T) {
		hex := strings.Repeat("a1b2c3d4", 6)
		input := []byte(`{"hash":"` + hex + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "high-entropy")
	})
	t.Run("long base64 positive", func(t *testing.T) {
		b64 := strings.Repeat("ABCDEFGHabcdefgh01234567", 3)
		input := []byte(`{"token":"` + b64 + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("near-miss short hex", func(t *testing.T) {
		input := []byte(`{"id":"deadbeef1234"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("near-miss normal text", func(t *testing.T) {
		input := []byte(`{"message":"hello world this is normal text with some words"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_KeyGlob_Password(t *testing.T) {
	t.Run("password key", func(t *testing.T) {
		input := []byte(`{"password":"mysecretpass"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "mysecretpass"))
		assert.Contains(t, findingPaths(result.Findings), "password")
		assert.Contains(t, findingReasons(result.Findings), "sensitive-key")
	})
	t.Run("passwd key", func(t *testing.T) {
		input := []byte(`{"passwd":"hunter2"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "hunter2"))
	})
	t.Run("PASSWORD uppercase", func(t *testing.T) {
		input := []byte(`{"PASSWORD":"Hunter2"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
}

func TestRedact_KeyGlob_Secret(t *testing.T) {
	input := []byte(`{"client_secret":"abc123xyz"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "abc123xyz"))
	assert.Contains(t, findingReasons(result.Findings), "sensitive-key")
}

func TestRedact_KeyGlob_Token(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"token", "tok_val1"},
		{"access_token", "tok_val2"},
		{"refresh_token", "tok_val3"},
		{"session_key", "ses_val1"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
			assert.False(t, containsSecret(result.Data, tc.value), "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_APIKey(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"api_key", "api_val1"},
		{"apikey", "api_val2"},
		{"api-key", "api_val3"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
			assert.False(t, containsSecret(result.Data, tc.value), "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_Authorization(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"authorization", "auth_header_val"},
		{"auth", "auth_val"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
			assert.False(t, containsSecret(result.Data, tc.value), "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_Credential(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"credential", "cred_val1"},
		{"credentials", "cred_val2"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_PrivateKey(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"private_key", "pk_val1"},
		{"private-key", "pk_val2"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_NestedObject(t *testing.T) {
	input := []byte(`{"config":{"database":{"password":"deep_secret","host":"localhost"}}}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "deep_secret"))
	paths := findingPaths(result.Findings)
	found := false
	for _, p := range paths {
		if strings.Contains(p, "password") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected finding path containing 'password', got %v", paths)
}

func TestRedact_KeyGlob_NestedArray(t *testing.T) {
	input := []byte(`{"users":[{"name":"Alice","token":"secret_tok_1"},{"name":"Bob","token":"secret_tok_2"}]}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "secret_tok_1"))
	assert.False(t, containsSecret(result.Data, "secret_tok_2"))
	paths := findingPaths(result.Findings)
	assert.Len(t, paths, 2)
}

func TestRedact_Mixed_ValueAndKey(t *testing.T) {
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	input := []byte(`{"password":"mypass","api_key":"` + awsKey + `"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "mypass"))
	assert.False(t, containsSecret(result.Data, awsKey))
	assert.GreaterOrEqual(t, len(result.Findings), 2)
}

func TestRedact_PlainText_NotJSON(t *testing.T) {
	t.Run("plain text with secret", func(t *testing.T) {
		secret := "AKIAIOSFODNN7EXAMPLE"
		input := []byte("export AWS_KEY=" + secret)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, secret))
	})
	t.Run("plain text no secret", func(t *testing.T) {
		input := []byte("hello world, this is normal text")
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("plain text multiple secrets exercises sort", func(t *testing.T) {
		awsKey := "AKIAIOSFODNN7EXAMPLE"
		ghToken := "ghp_" + strings.Repeat("z", 36)
		input := []byte("AWS=" + awsKey + " GH=" + ghToken)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, awsKey))
		assert.False(t, containsSecret(result.Data, ghToken))
		assert.GreaterOrEqual(t, len(result.Findings), 2)
	})
}

func TestRedact_MalformedJSON_GracefulFallback(t *testing.T) {
	input := []byte(`{"key": "value", "broken":}`)
	result := redact.Redact(input)
	require.NotNil(t, result.Data)
}

func TestRedact_Unicode(t *testing.T) {
	t.Run("unicode content no secret", func(t *testing.T) {
		input := []byte(`{"greeting":"こんにちは世界","value":42}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
		assert.Contains(t, string(result.Data), "こんにちは世界")
	})
	t.Run("unicode with secret key", func(t *testing.T) {
		input := []byte(`{"password":"パスワード123","greeting":"こんにちは"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "パスワード123"))
	})
}

func TestRedact_FindingsSorted(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	input := []byte(`{"password":"pass123","token":"` + jwt + `","api_key":"AKIAIOSFODNN7EXAMPLE"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.GreaterOrEqual(t, len(result.Findings), 2)
	paths := findingPaths(result.Findings)
	assert.True(t, sort.StringsAreSorted(paths), "expected sorted findings, got %v", paths)
}

func TestRedact_KeyGlob_NonStringValue(t *testing.T) {
	input := []byte(`{"password":12345,"api_key":null,"secret":true}`)
	result := redact.Redact(input)
	assert.False(t, result.Redacted)
}

func TestRedact_KeyGlob_ClientSecret(t *testing.T) {
	input := []byte(`{"client_secret":"verysecretvalue"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "verysecretvalue"))
}

func TestRedact_KeyGlob_AccessToken(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"access_token", "access_val1"},
		{"access-token", "access_val2"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
		})
	}
}

func TestRedact_KeyGlob_RefreshToken(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"refresh_token", "refresh_val1"},
		{"refresh-token", "refresh_val2"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			input := []byte(`{"` + tc.key + `":"` + tc.value + `"}`)
			result := redact.Redact(input)
			assert.True(t, result.Redacted, "key=%s", tc.key)
		})
	}
}

func TestRedact_ResultRedactedField(t *testing.T) {
	t.Run("no findings means Redacted false", func(t *testing.T) {
		input := []byte(`{"name":"Alice"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
		assert.Empty(t, result.Findings)
	})
	t.Run("with findings means Redacted true", func(t *testing.T) {
		input := []byte(`{"password":"secret"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.NotEmpty(t, result.Findings)
	})
}
