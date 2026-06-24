package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Duration(ms *int64) string {
	if ms == nil {
		return "—"
	}
	v := *ms
	switch {
	case v < 1000:
		return fmt.Sprintf("%dms", v)
	case v < 60000:
		return fmt.Sprintf("%.1fs", float64(v)/1000)
	case v < 3600000:
		return fmt.Sprintf("%dm %02ds", v/60000, (v%60000)/1000)
	default:
		return fmt.Sprintf("%dh %02dm", v/3600000, (v%3600000)/60000)
	}
}

func Tokens(n *int64) string {
	if n == nil {
		return "—"
	}
	v := *n
	switch {
	case v == 0:
		return "0"
	case v >= 10000:
		return fmt.Sprintf("%.1fk", float64(v)/1000)
	default:
		return withThousands(v)
	}
}

func Cost(usd *float64) string {
	if usd == nil {
		return "—"
	}
	v := *usd
	switch {
	case v == 0:
		return "$0.00"
	case v < 0.01:
		return fmt.Sprintf("$%.4f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

func ShortHash(h string, n int) string {
	if h == "" {
		return "—"
	}
	r := []rune(h)
	if n >= len(r) {
		return h
	}
	return string(r[:n])
}

func Date(iso string) string {
	if iso == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return "—"
	}
	return t.Local().Format("Jan 2 15:04")
}

func withThousands(v int64) string {
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
