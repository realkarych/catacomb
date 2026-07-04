package model

import "regexp"

const maxRunIDLen = 256

var runIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidRunID(s string) bool {
	return s != "" && len(s) <= maxRunIDLen && runIDRe.MatchString(s)
}
