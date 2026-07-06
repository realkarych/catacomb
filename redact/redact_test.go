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
	input := []byte(`{"note":"ghp_abcdefghijklmnopqrstuvwxyz1234567890","name":"Alice"}`)
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
	t.Run("AKIA positive under non-sensitive key", func(t *testing.T) {
		secret := "AKIAIOSFODNN7EXAMPLE"
		input := []byte(`{"note":"` + secret + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, secret))
		reasons := findingReasons(result.Findings)
		assert.Contains(t, reasons, "aws-key")
	})
	t.Run("ASIA STS positive under non-sensitive key", func(t *testing.T) {
		secret := "ASIAJEXAMPLEKEY12345"
		input := []byte(`{"note":"` + secret + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "ASIA STS key must be caught by value-scan")
		assert.False(t, containsSecret(result.Data, secret))
		assert.Contains(t, findingReasons(result.Findings), "aws-key")
	})
	t.Run("AROA positive under non-sensitive key", func(t *testing.T) {
		secret := "AROAJEXAMPLEKEY12345"
		input := []byte(`{"note":"` + secret + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "aws-key")
	})
	t.Run("AIDA positive under non-sensitive key", func(t *testing.T) {
		secret := "AIDAJEXAMPLEKEY12345"
		input := []byte(`{"note":"` + secret + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "aws-key")
	})
	t.Run("near-miss wrong prefix", func(t *testing.T) {
		input := []byte(`{"note":"BKIAIOSFODNN7EXAMPLE"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"note":"AKIA123"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_GitHub_Token(t *testing.T) {
	t.Run("ghp positive under non-sensitive key", func(t *testing.T) {
		token := "ghp_" + strings.Repeat("a", 36)
		input := []byte(`{"note":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, token))
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
	t.Run("gho positive under non-sensitive key", func(t *testing.T) {
		token := "gho_" + strings.Repeat("b", 36)
		input := []byte(`{"content":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
	t.Run("ghs positive under non-sensitive key", func(t *testing.T) {
		token := "ghs_" + strings.Repeat("c", 36)
		input := []byte(`{"text":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
	t.Run("ghu positive under non-sensitive key", func(t *testing.T) {
		token := "ghu_" + strings.Repeat("d", 36)
		input := []byte(`{"content":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"value":"ghp_short"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("github_pat positive under non-sensitive key", func(t *testing.T) {
		token := "github_pat_" + strings.Repeat("f", 40)
		input := []byte(`{"note":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "github-token")
	})
}

func TestRedact_ValueScan_OpenAI_Key(t *testing.T) {
	t.Run("sk- basic positive under non-sensitive key", func(t *testing.T) {
		key := "sk-" + strings.Repeat("A", 20)
		input := []byte(`{"note":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, key))
		assert.Contains(t, findingReasons(result.Findings), "openai-key")
	})
	t.Run("sk-ant- positive under non-sensitive key", func(t *testing.T) {
		key := "sk-ant-" + strings.Repeat("B", 20)
		input := []byte(`{"note":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "openai-key")
	})
	t.Run("sk-ant- with underscore positive under non-sensitive key", func(t *testing.T) {
		key := "sk-ant-api03-AbCDef_GHIjkl_MNOpqr_STUvwx_YZabcd_EFAAAA"
		input := []byte(`{"note":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "sk-ant- key containing underscores must be caught by value-scan")
		assert.False(t, containsSecret(result.Data, key))
		assert.Contains(t, findingReasons(result.Findings), "openai-key")
	})
	t.Run("sk-proj- with underscore positive under non-sensitive key", func(t *testing.T) {
		key := "sk-proj-ab_cd-ef_gh-ij_kl-mn_op-qr_st-uv_wx-yz_AA"
		input := []byte(`{"note":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "sk-proj- key containing underscores must be caught by value-scan")
		assert.False(t, containsSecret(result.Data, key))
		assert.Contains(t, findingReasons(result.Findings), "openai-key")
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"note":"sk-short"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_Slack_Token(t *testing.T) {
	t.Run("xoxb positive under non-sensitive key", func(t *testing.T) {
		token := "xoxb-" + strings.Repeat("1", 10)
		input := []byte(`{"note":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "slack-token")
	})
	t.Run("xoxa positive under non-sensitive key", func(t *testing.T) {
		token := "xoxa-" + strings.Repeat("2", 10)
		input := []byte(`{"content":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "slack-token")
	})
	t.Run("xoxp positive under non-sensitive key", func(t *testing.T) {
		token := "xoxp-" + strings.Repeat("3", 10)
		input := []byte(`{"text":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "slack-token")
	})
	t.Run("xoxr positive under non-sensitive key", func(t *testing.T) {
		token := "xoxr-" + strings.Repeat("4", 10)
		input := []byte(`{"note":"` + token + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
	t.Run("xoxs positive under non-sensitive key", func(t *testing.T) {
		token := "xoxs-" + strings.Repeat("5", 10)
		input := []byte(`{"note":"` + token + `"}`)
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
	t.Run("positive under non-sensitive key", func(t *testing.T) {
		jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
		input := []byte(`{"note":"` + jwt + `"}`)
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
	t.Run("positive RSA marker in JSON string value", func(t *testing.T) {
		pem := "-----BEGIN RSA PRIVATE KEY-----"
		input := []byte(`{"note":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, pem))
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
	t.Run("positive EC marker in JSON string value", func(t *testing.T) {
		pem := "-----BEGIN EC PRIVATE KEY-----"
		input := []byte(`{"note":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
	t.Run("positive generic PRIVATE KEY marker in JSON string value", func(t *testing.T) {
		pem := "-----BEGIN PRIVATE KEY-----"
		input := []byte(`{"note":"` + pem + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
	t.Run("near-miss public key", func(t *testing.T) {
		input := []byte(`{"note":"-----BEGIN PUBLIC KEY-----"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
	t.Run("full PEM block body redacted in free text", func(t *testing.T) {
		pemBlock := "-----BEGIN RSA PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\nSHORTSECRET123\n-----END RSA PRIVATE KEY-----"
		input := []byte(pemBlock)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "full PEM block must be caught in free-text mode")
		assert.False(t, containsSecret(result.Data, "SHORTSECRET123"), "PEM body must be redacted in free-text mode")
		assert.False(t, containsSecret(result.Data, "b3BlbnNzaC1rZXktdjEAAAAA"), "PEM body must be fully redacted")
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
	t.Run("OPENSSH full block body redacted in free text", func(t *testing.T) {
		pemBlock := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\nSHORTSECRET123\n-----END OPENSSH PRIVATE KEY-----"
		input := []byte(pemBlock)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "SHORTSECRET123"))
	})
	t.Run("PEM marker only without END block redacted in free text", func(t *testing.T) {
		markerOnly := "-----BEGIN RSA PRIVATE KEY-----\nsome body without end marker"
		input := []byte(markerOnly)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "PEM marker alone must be caught in free-text mode")
		assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
	})
}

func TestRedact_ValueScan_Google_API_Key(t *testing.T) {
	t.Run("positive under non-sensitive key", func(t *testing.T) {
		key := "AIza" + strings.Repeat("B", 35)
		input := []byte(`{"note":"` + key + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "google-api-key")
	})
	t.Run("near-miss short", func(t *testing.T) {
		input := []byte(`{"note":"AIzaShort"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_Bearer_Token(t *testing.T) {
	t.Run("positive under non-sensitive key", func(t *testing.T) {
		input := []byte(`{"note":"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.XXXX"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "bearer-token")
	})
	t.Run("near-miss no token value", func(t *testing.T) {
		input := []byte(`{"note":"Bearer "}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_ConnectionString(t *testing.T) {
	t.Run("postgres positive under non-sensitive key", func(t *testing.T) {
		connstr := "postgres://user:secretpass@localhost:5432/db"
		input := []byte(`{"note":"` + connstr + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, connstr))
		assert.Contains(t, findingReasons(result.Findings), "connection-string")
	})
	t.Run("mysql positive under non-sensitive key", func(t *testing.T) {
		connstr := "mysql://admin:pass123@db.example.com/mydb"
		input := []byte(`{"content":"` + connstr + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "connection-string")
	})
	t.Run("password-only redis positive under non-sensitive key", func(t *testing.T) {
		connstr := "redis://:secretpass@localhost:6379"
		input := []byte(`{"note":"` + connstr + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "connection-string")
	})
	t.Run("near-miss no password", func(t *testing.T) {
		input := []byte(`{"note":"postgres://localhost:5432/db"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted)
	})
}

func TestRedact_ValueScan_HighEntropy(t *testing.T) {
	t.Run("long hex positive under non-sensitive key", func(t *testing.T) {
		hex := strings.Repeat("a1b2c3d4", 6)
		input := []byte(`{"note":"` + hex + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "high-entropy")
	})
	t.Run("long base64 positive under non-sensitive key", func(t *testing.T) {
		b64 := strings.Repeat("ABCDEFGHabcdefgh01234567", 3)
		input := []byte(`{"note":"` + b64 + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, findingReasons(result.Findings), "high-entropy")
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
	t.Run("long absolute file path not redacted", func(t *testing.T) {
		path := "/Users/karych/src/observability/web/src/components/SessionView.svelte"
		input := []byte(`{"file_path":"` + path + `"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted, "absolute file path must not be redacted as high-entropy")
		assert.Empty(t, result.Findings)
	})
	t.Run("another long absolute path not redacted", func(t *testing.T) {
		path := "/Users/somebody/projects/my-awesome-project/internal/handlers/authentication.go"
		input := []byte(`{"note":"` + path + `"}`)
		result := redact.Redact(input)
		assert.False(t, result.Redacted, "absolute file path must not be redacted as high-entropy")
	})
	t.Run("base64 blob with sparse slash still redacted via contiguous run", func(t *testing.T) {
		blob := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqr/stuvwxyz01234567"
		input := []byte(`{"note":"` + blob + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "base64 blob with sparse slash must still be redacted via its contiguous >=40 run")
		assert.Contains(t, findingReasons(result.Findings), "high-entropy")
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
	t.Run("db_password compound key", func(t *testing.T) {
		input := []byte(`{"db_password":"mysupersecret"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "db_password must be caught by compound key matching")
		assert.False(t, containsSecret(result.Data, "mysupersecret"))
	})
	t.Run("user_password compound key", func(t *testing.T) {
		input := []byte(`{"user_password":"hunter3"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
}

func TestRedact_KeyGlob_Secret(t *testing.T) {
	t.Run("client_secret exact", func(t *testing.T) {
		input := []byte(`{"client_secret":"abc123xyz"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, "abc123xyz"))
		assert.Contains(t, findingReasons(result.Findings), "sensitive-key")
	})
	t.Run("aws_secret_access_key compound", func(t *testing.T) {
		input := []byte(`{"aws_secret_access_key":"wJalrXUtnFEMI"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "aws_secret_access_key must be caught by compound key matching")
		assert.False(t, containsSecret(result.Data, "wJalrXUtnFEMI"))
	})
	t.Run("client_secret_key compound", func(t *testing.T) {
		input := []byte(`{"client_secret_key":"somesecretval"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
	})
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
		{"x-api-key", "api_val4"},
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
	input := []byte(`{"password":"mypass","note":"` + awsKey + `"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.False(t, containsSecret(result.Data, "mypass"))
	assert.False(t, containsSecret(result.Data, awsKey))
	assert.GreaterOrEqual(t, len(result.Findings), 2)
}

func TestRedact_PlainText_NotJSON(t *testing.T) {
	t.Run("plain text with AKIA secret", func(t *testing.T) {
		secret := "AKIAIOSFODNN7EXAMPLE"
		input := []byte("export AWS_KEY=" + secret)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, secret))
	})
	t.Run("plain text with ASIA STS secret", func(t *testing.T) {
		secret := "ASIAJEXAMPLEKEY12345"
		input := []byte("export AWS_KEY=" + secret)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.False(t, containsSecret(result.Data, secret))
	})
	t.Run("plain text with sk-ant underscore key", func(t *testing.T) {
		secret := "sk-ant-api03-AbCDef_GHIjkl_MNOpqr_STUvwxAA"
		input := []byte("ANTHROPIC_API_KEY=" + secret)
		result := redact.Redact(input)
		assert.True(t, result.Redacted, "sk-ant- key with underscores must be caught in free-text mode")
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
	input := []byte(`{"password":"pass123","note":"` + jwt + `","api_key":"AKIAIOSFODNN7EXAMPLE"}`)
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

func TestRedact_Placeholder_Format(t *testing.T) {
	t.Run("sensitive key placeholder includes reason", func(t *testing.T) {
		input := []byte(`{"password":"mysecret"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, string(result.Data), "‹redacted:")
		assert.NotContains(t, string(result.Data), "mysecret")
	})
	t.Run("value scan placeholder includes reason", func(t *testing.T) {
		secret := "AKIAIOSFODNN7EXAMPLE"
		input := []byte(`{"note":"` + secret + `"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, string(result.Data), "‹redacted:aws-key›")
	})
}

func TestRedact_NumberFidelity(t *testing.T) {
	t.Run("big integer preserved when redaction occurs", func(t *testing.T) {
		input := []byte(`{"password":"s3cr3t","id":10000000000000001}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, string(result.Data), "10000000000000001", "big integer must not be corrupted by float64 round-trip")
	})
	t.Run("large float preserved when redaction occurs", func(t *testing.T) {
		input := []byte(`{"password":"s3cr3t","ratio":1.23456789012345}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, string(result.Data), "1.23456789012345")
	})
	t.Run("HTML characters not escaped", func(t *testing.T) {
		input := []byte(`{"password":"s3cr3t","url":"https://example.com?a=1&b=2"}`)
		result := redact.Redact(input)
		assert.True(t, result.Redacted)
		assert.Contains(t, string(result.Data), "&", "ampersand must not be HTML-escaped in output")
		assert.NotContains(t, string(result.Data), "\\u0026")
	})
}

func TestRedactJSONValueReplacesOnlyMatchedSpan(t *testing.T) {
	sha := strings.Repeat("0123456789abcdef", 2) + "01234567"
	input := []byte(`{"command":"git checkout ` + sha + ` && make test"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.JSONEq(t, `{"command":"git checkout ‹redacted:high-entropy› && make test"}`, string(result.Data))
	assert.Contains(t, findingReasons(result.Findings), "high-entropy")

	twice := redact.Redact(result.Data)
	assert.Equal(t, string(result.Data), string(twice.Data), "span-level value redaction must be idempotent")
	assert.False(t, twice.Redacted)
}

func TestRedactJSONValueMultipleRulesInOneValue(t *testing.T) {
	input := []byte(`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb && export K=AKIAIOSFODNN7EXAMPLE"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.NotContains(t, string(result.Data), "kesha_dev_password")
	assert.NotContains(t, string(result.Data), "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, string(result.Data), "‹redacted:connection-string›")
	assert.Contains(t, string(result.Data), "‹redacted:aws-key›")
	assert.Contains(t, string(result.Data), "export K=")
	reasons := findingReasons(result.Findings)
	assert.Contains(t, reasons, "connection-string")
	assert.Contains(t, reasons, "aws-key")
	assert.Equal(t, findingPaths(result.Findings), []string{"command", "command"})
}

func TestRedactSensitiveKeyStillReplacesWholeValue(t *testing.T) {
	input := []byte(`{"password":"prefix AKIAIOSFODNN7EXAMPLE suffix"}`)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.JSONEq(t, `{"password":"‹redacted:aws-key›"}`, string(result.Data))
}

func TestRedactJSONValuePEMBlockSpan(t *testing.T) {
	input, err := json.Marshal(map[string]string{
		"script": "cat key.pem\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\necho done",
	})
	require.NoError(t, err)
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	assert.NotContains(t, string(result.Data), "MIIEow")
	assert.Contains(t, string(result.Data), "cat key.pem")
	assert.Contains(t, string(result.Data), "echo done")
	assert.Contains(t, findingReasons(result.Findings), "pem-private-key")
}

func TestRedactFixedPoint(t *testing.T) {
	cases := []string{
		`{"api_key":"AKIAIOSFODNN7EXAMPLE"}`,
		`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`,
		`export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE && curl -H "Authorization: Bearer abcdefghij1234567890"`,
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		`token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dozjgNryP4J3jVmNHl0w5N7XgL0n3I9PlFUP0THsR8U`,
		`AKIAIOSFODNN7EXAMPLE0123456789abcdef0123456789abcdef01234567`,
		`{"nested":{"password":"hunter2","cwd":"/home/kesha"},"n":3}`,
		`{"command":"git checkout ` + strings.Repeat("ab", 20) + ` && make test"}`,
		`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb && export K=AKIAIOSFODNN7EXAMPLE"}`,
		string([]byte{0xff, 0xfe, 0x01}),
		`{"text":"no secrets here"}`,
		`plain prose without any secret`,
		"{\"password\":\"foo -----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq\n-----END PRIVATE KEY----- bar\"}",
		"{\"password\":\"foo postgres://user:pa\x01ss@host/db bar\"}",
	}
	for _, in := range cases {
		once := redact.Redact([]byte(in))
		twice := redact.Redact(once.Data)
		assert.Equal(t, string(once.Data), string(twice.Data), "input %q", in)
		assert.False(t, twice.Redacted, "second pass must be a no-op for %q", in)
		assert.Empty(t, twice.Findings, "input %q", in)
	}
}

func TestRedactFixedPointFreeTextHealing(t *testing.T) {
	cases := []string{
		"{\"password\":\"foo -----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq\n-----END PRIVATE KEY----- bar\"}",
		"{\"password\":\"foo postgres://user:pa\x01ss@host/db bar\"}",
	}
	for _, in := range cases {
		once := redact.Redact([]byte(in))
		twice := redact.Redact(once.Data)
		assert.Equal(t, string(once.Data), string(twice.Data), "redact(redact(x)) must equal redact(x) for %q", in)
		assert.True(t, once.Redacted, "input %q must be redacted", in)
		assert.False(t, containsSecret(once.Data, "postgres://user"), "connection string must not survive in %q", in)
	}
}

func TestRedactMergesDuplicateFindingsAcrossPasses(t *testing.T) {
	input := []byte("ghp_" + strings.Repeat("z", 36) + " and github_pat_" + strings.Repeat("A", 40))
	result := redact.Redact(input)
	assert.True(t, result.Redacted)
	require.Len(t, result.Findings, 1, "two github-token hits must dedupe to one finding")
	assert.Equal(t, "github-token", result.Findings[0].Reason)
}

func TestRedactPreservesTypedRefUnderSensitiveKey(t *testing.T) {
	cases := []string{
		`{"token":"‹ref:262144,fedcba9876543210›"}`,
		`{"password":"‹binary:1048576,0123456789abcdef›"}`,
	}
	for _, in := range cases {
		r := redact.Redact([]byte(in))
		assert.False(t, r.Redacted, "typed ref under a sensitive key must not be re-wrapped: %q", in)
		assert.Equal(t, in, string(r.Data), "typed ref under a sensitive key must stay byte-identical: %q", in)
		assert.Empty(t, r.Findings, "%q", in)
		twice := redact.Redact(r.Data)
		assert.Equal(t, in, string(twice.Data), "re-redaction must stay byte-identical: %q", in)
	}
}

func TestRedactAnchorsTypedRefLookalikesUnderSensitiveKey(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"trailing garbage after ref", "‹ref:1,ab›garbage"},
		{"leading char before binary ref", "x‹binary:3,abc›"},
		{"ref with trailing newline", "‹ref:1,ab›\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"token": tc.value})
			require.NoError(t, err)
			result := redact.Redact(raw)
			assert.True(t, result.Redacted, "typed-ref lookalike %q must still be redacted", tc.value)
			assert.False(t, containsSecret(result.Data, tc.value), "lookalike %q must not survive byte-identical", tc.value)
			assert.Contains(t, string(result.Data), "‹redacted:", "lookalike %q must be wrapped", tc.value)
		})
	}
}

func TestHasMarker(t *testing.T) {
	assert.True(t, redact.HasMarker([]byte(`{"x":"‹redacted:aws-key›"}`)))
	assert.True(t, redact.HasMarker([]byte(`"‹binary:3,0123456789abcdef›"`)))
	assert.True(t, redact.HasMarker([]byte(`"‹ref:99,0123456789abcdef›"`)))
	assert.False(t, redact.HasMarker([]byte(`{"x":"clean"}`)))
	assert.False(t, redact.HasMarker(nil))
}
