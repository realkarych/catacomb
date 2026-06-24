package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func TestDuration(t *testing.T) {
	cases := []struct {
		in   *int64
		want string
	}{
		{nil, "—"},
		{i64(0), "0ms"},
		{i64(820), "820ms"},
		{i64(999), "999ms"},
		{i64(1000), "1.0s"},
		{i64(1400), "1.4s"},
		{i64(59999), "60.0s"},
		{i64(60000), "1m 00s"},
		{i64(123000), "2m 03s"},
		{i64(3599000), "59m 59s"},
		{i64(3600000), "1h 00m"},
		{i64(3840000), "1h 04m"},
		{i64(7260000), "2h 01m"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Duration(c.in))
	}
}

func TestTokens(t *testing.T) {
	cases := []struct {
		in   *int64
		want string
	}{
		{nil, "—"},
		{i64(0), "0"},
		{i64(999), "999"},
		{i64(1000), "1,000"},
		{i64(9999), "9,999"},
		{i64(10000), "10.0k"},
		{i64(12345), "12.3k"},
		{i64(123456), "123.5k"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Tokens(c.in))
	}
}

func TestCost(t *testing.T) {
	cases := []struct {
		in   *float64
		want string
	}{
		{nil, "—"},
		{f64(0), "$0.00"},
		{f64(0.0012), "$0.0012"},
		{f64(0.0123), "$0.01"},
		{f64(0.12), "$0.12"},
		{f64(1.23), "$1.23"},
		{f64(0.009), "$0.0090"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Cost(c.in))
	}
}

func TestShortHash(t *testing.T) {
	assert.Equal(t, "—", ShortHash("", 8))
	assert.Equal(t, "sha-1234", ShortHash("sha-1234abcdef", 8))
	assert.Equal(t, "abc", ShortHash("abc", 8))
	assert.Equal(t, "sha-", ShortHash("sha-1234abcdef", 4))
	assert.Equal(t, "abc", ShortHash("abc", 10))
}

func TestDate(t *testing.T) {
	assert.Equal(t, "—", Date(""))
	assert.Equal(t, "—", Date("not-a-date"))
	assert.NotEqual(t, "—", Date("2026-06-20T10:00:01Z"))
}
