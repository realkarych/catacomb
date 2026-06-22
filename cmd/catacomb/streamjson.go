package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
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

func streamForward(warn io.Writer, discoveryPath string, body io.Reader) {
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
			streamForward(cmd.ErrOrStderr(), daemon.DiscoveryPath(), cmd.InOrStdin())
			return nil
		},
	}
	cmd.AddCommand(sj)
	return cmd
}

func newRunCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "run -- <cmd...>",
		Short: "Run a Claude Code command, tee its stream-json to the terminal and the daemon",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChild(cmd.OutOrStdout(), cmd.ErrOrStderr(), daemon.DiscoveryPath(), runID, args)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "CATACOMB_RUN_ID value exported to the child for multi-session grouping")
	return cmd
}

func runChild(stdout, stderr io.Writer, discoveryPath, runID string, args []string) error {
	child := execCommand(args[0], args[1:]...)
	child.Stdin = os.Stdin
	child.Env = os.Environ()
	if runID != "" {
		child.Env = append(child.Env, "CATACOMB_RUN_ID="+runID)
	}
	pr, pw := io.Pipe()
	lossy := &lossyWriter{ch: make(chan []byte, streamTeeBuffer)}
	child.Stdout = io.MultiWriter(stdout, lossy)
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
		streamForward(stderr, discoveryPath, pr)
		_, _ = io.Copy(io.Discard, pr)
	}()
	if err := child.Start(); err != nil {
		close(lossy.ch)
		_ = pw.Close()
		<-pumpDone
		<-fwdDone
		_ = pr.Close()
		return err
	}
	waitErr := child.Wait()
	close(lossy.ch)
	_ = pw.Close()
	<-pumpDone
	<-fwdDone
	_ = pr.Close()
	return waitErr
}
