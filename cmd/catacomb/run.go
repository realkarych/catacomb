package main

import (
	"errors"
	"fmt"
	"io"
)

func run(args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	if errors.Is(err, errRegressionDetected) {
		return 1
	}
	fmt.Fprintln(stderr, renderErr(err))
	var opErr *operationalError
	if errors.As(err, &opErr) {
		return 2
	}
	return 1
}
