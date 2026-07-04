package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/model"
)

const streamTeeBuffer = 1024

var (
	execCommand      = exec.Command
	streamHTTPClient = &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
)

type lossyWriter struct {
	ch      chan []byte
	dropped atomic.Int64
}

func (w *lossyWriter) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case w.ch <- b:
	default:
		w.dropped.Add(1)
	}
	return len(p), nil
}

func streamForward(warn io.Writer, discoveryPath string, body io.Reader, labels, runID string) {
	d, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: discovery: %v\n", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+d.Addr+"/v1/stream-json", body)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: request: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	if labels != "" {
		req.Header.Set("X-Catacomb-Labels", labels)
	}
	if model.ValidRunID(runID) {
		req.Header.Set("X-Catacomb-Run-ID", runID)
	}
	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: forward to %s: %v\n", d.Addr, err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(warn, "catacomb stream-json: forward to %s: status %d\n", d.Addr, resp.StatusCode)
	}
}

func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Forward Claude Code output to the catacomb daemon",
	}
	sj := &cobra.Command{
		Use:   "stream-json",
		Short: "Forward stream-json NDJSON from stdin to the catacomb daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			streamForward(cmd.ErrOrStderr(), clientDiscoveryPath(), cmd.InOrStdin(),
				os.Getenv("CATACOMB_LABELS"), os.Getenv("CATACOMB_RUN_ID"))
			return nil
		},
	}
	cmd.AddCommand(sj)
	return cmd
}

func newRunCmd() *cobra.Command {
	var runID string
	var labels []string
	cmd := &cobra.Command{
		Use:   "run -- <cmd...>",
		Short: "Run a Claude Code command, tee its stream-json to the terminal and the daemon",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateLabelTerms(labels); err != nil {
				return err
			}
			if runID != "" && !model.ValidRunID(runID) {
				return fmt.Errorf("invalid --run-id %q: expected [A-Za-z0-9._-]{1,256}", runID)
			}
			merged := model.MergeLabels(model.ParseLabels(os.Getenv("CATACOMB_LABELS")), model.ParseLabels(strings.Join(labels, ",")))
			canonical := model.FormatLabels(merged)
			return runChild(cmd.OutOrStdout(), cmd.ErrOrStderr(), clientDiscoveryPath(), runID, args, canonical)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "CATACOMB_RUN_ID value exported to the child for multi-session grouping")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "k=v label recorded on the run (repeatable; adds to CATACOMB_LABELS)")
	return cmd
}

const maxObserverBuffer = 1 << 20

type lineObserver struct {
	buf     []byte
	stopped bool
	observe func(line []byte)
}

func (w *lineObserver) Write(p []byte) (int, error) {
	if w.stopped {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.observe(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxObserverBuffer {
		w.buf = nil
		w.stopped = true
	}
	return len(p), nil
}

func (w *lineObserver) flush() {
	if w.stopped || len(w.buf) == 0 {
		return
	}
	w.observe(w.buf)
	w.buf = nil
}

func runChild(stdout, stderr io.Writer, discoveryPath, runID string, args []string, labels string) error {
	return runChildObserved(stdout, stderr, discoveryPath, runID, args, labels, "", nil, nil)
}

func runChildObserved(stdout, stderr io.Writer, discoveryPath, runID string, args []string, labels, dir string, extraEnv []string, observe func(line []byte)) error {
	child := execCommand(args[0], args[1:]...)
	child.Stdin = os.Stdin
	child.Dir = dir
	child.Env = os.Environ()
	child.Env = append(child.Env, extraEnv...)
	if runID != "" {
		child.Env = append(child.Env, "CATACOMB_RUN_ID="+runID)
	}
	if labels != "" {
		child.Env = append(child.Env, "CATACOMB_LABELS="+labels)
	}
	pr, pw := io.Pipe()
	lossy := &lossyWriter{ch: make(chan []byte, streamTeeBuffer)}
	writers := []io.Writer{stdout, lossy}
	var obs *lineObserver
	if observe != nil {
		obs = &lineObserver{observe: observe}
		writers = append(writers, obs)
	}
	child.Stdout = io.MultiWriter(writers...)
	child.Stderr = stderr
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for b := range lossy.ch {
			if _, err := pw.Write(b); err != nil {
				return
			}
		}
	}()
	fwdDone := make(chan struct{})
	go func() {
		defer close(fwdDone)
		streamForward(stderr, discoveryPath, pr, labels, runID)
		_, _ = io.Copy(io.Discard, pr)
	}()
	teardown := func() {
		if obs != nil {
			obs.flush()
		}
		close(lossy.ch)
		<-pumpDone
		_ = pw.Close()
		<-fwdDone
		_ = pr.Close()
	}
	if err := child.Start(); err != nil {
		teardown()
		return err
	}
	waitErr := child.Wait()
	teardown()
	return waitErr
}
