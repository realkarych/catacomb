package redact_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/redact"
)

func FuzzRedact(f *testing.F) {
	f.Add([]byte(`{"token":"sk_live_ABCDEFGHIJKLMNOP0123"}`))
	f.Add([]byte("plain text AKIAABCDEFGHIJKLMNOP"))
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
