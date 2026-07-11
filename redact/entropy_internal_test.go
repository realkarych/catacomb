package redact

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShannonEntropy(t *testing.T) {
	require.Equal(t, 0.0, shannonEntropy(""))
	require.Equal(t, 0.0, shannonEntropy("aaaa"))
	require.InDelta(t, 1.0, shannonEntropy("abab"), 1e-9)
	require.Greater(t, shannonEntropy("9f86d081884c7d659a2feaa0c55ad015"), 3.2)
	require.Less(t, shannonEntropy("deadbeefcafe1234deadbeefcafe1234"), 3.2)
	require.Less(t, shannonEntropy("thisisalonglowercaseenglishlooking"), 4.0)
}

func TestEntropyGatedDetection(t *testing.T) {
	secret := "a1B2c3D4e5F6g7H8j9K0mN1pQ2rS3tU4"
	got := string(Redact([]byte(`{"v":"` + secret + `"}`)).Data)
	require.Contains(t, got, placeholder("high-entropy"))
	require.NotContains(t, got, secret)

	lowEntropy := strings.Repeat("ab", 20)
	clean := string(Redact([]byte(`{"v":"` + lowEntropy + `"}`)).Data)
	require.Equal(t, `{"v":"`+lowEntropy+`"}`, clean)
}

func TestEntropyGatedDetectionBase64URL(t *testing.T) {
	secret := "q7Wz-e2Rv_t9Yb-u4Ix_o6Pn-a3Sm_d8Fc-g5Hk"
	got := string(Redact([]byte(`{"v":"` + secret + `"}`)).Data)
	require.Contains(t, got, placeholder("high-entropy"))
	require.NotContains(t, got, secret)

	for _, benign := range []string{
		"f10d7209-f2aa-4f86-a656-0d93f3ab12e9",
		"01234567-89ab-cdef-0123-456789abcdef",
		"my-awesome-project-internal-handlers",
	} {
		in := `{"v":"` + benign + `"}`
		result := Redact([]byte(in))
		require.False(t, result.Redacted, benign)
		require.Equal(t, in, string(result.Data), benign)
	}
}

func TestEntropyGatedDetectionSlashBase64(t *testing.T) {
	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	inJSON := string(Redact([]byte(`{"v":"` + secret + `"}`)).Data)
	require.Contains(t, inJSON, placeholder("high-entropy"))
	require.NotContains(t, inJSON, secret)

	free := Redact([]byte("export AWS_SECRET_ACCESS_KEY=" + secret))
	require.True(t, free.Redacted)
	require.Contains(t, string(free.Data), placeholder("high-entropy"))
	require.NotContains(t, string(free.Data), secret)

	for _, benign := range []string{
		"/Users/karych/src/observability/web/src/components/SessionView.svelte",
		"/Users/somebody/projects/my-awesome-project/internal/handlers/authentication.go",
		"/home/runner/work/catacomb/catacomb/redact/redact.go",
		"f10d7209-f2aa-4f86-a656-0d93f3ab12e9",
	} {
		in := `{"v":"` + benign + `"}`
		result := Redact([]byte(in))
		require.False(t, result.Redacted, benign)
		require.Equal(t, in, string(result.Data), benign)
	}
}

func TestEntropyGateSensitiveKeyClassifier(t *testing.T) {
	high := string(Redact([]byte(`{"token":"9f86d081884c7d659a2feaa0c55ad015"}`)).Data)
	require.Contains(t, high, placeholder("high-entropy"))

	low := string(Redact([]byte(`{"token":"` + strings.Repeat("ab", 20) + `"}`)).Data)
	require.Contains(t, low, placeholder("sensitive-key"))
}

func TestEntropyGateFreeText(t *testing.T) {
	low := strings.Repeat("ab", 20)
	in := "git checkout " + low + " then 9f86d081884c7d659a2feaa0c55ad015"
	result := Redact([]byte(in))
	require.True(t, result.Redacted)
	got := string(result.Data)
	require.Contains(t, got, low)
	require.Contains(t, got, placeholder("high-entropy"))
	require.NotContains(t, got, "9f86d081884c7d659a2feaa0c55ad015")

	untouched := Redact([]byte("git checkout " + low + " done"))
	require.False(t, untouched.Redacted)
}
