package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/realkarych/catacomb/daemon"
)

var (
	ErrDaemonUnreachable = errors.New("catacomb daemon is unreachable")
	ErrDaemonRestarted   = errors.New("catacomb daemon restarted (token mismatch)")
	ErrContentDisabled   = errors.New("content viewing disabled by the daemon")
	ErrSessionNotFound   = errors.New("session not found")
	ErrPayloadNotFound   = errors.New("payload not found")
)

type StreamMsg struct {
	Event SseEvent
	Err   error
	Done  bool
}

type Client interface {
	Sessions(ctx context.Context) ([]SessionSummary, error)
	Graph(ctx context.Context, hash string) ([]SseEvent, error)
	Subscribe(ctx context.Context, hash string, sinceRev uint64) (<-chan StreamMsg, error)
	Payload(ctx context.Context, hash, nodeID string) (PayloadView, error)
}

type HTTPClient struct {
	addr    string
	token   string
	http    *http.Client
	sseHTTP *http.Client
}

func NewHTTPClient(disc daemon.Discovery) *HTTPClient {
	return &HTTPClient{
		addr:    "http://" + disc.Addr,
		token:   disc.Token,
		http:    &http.Client{Timeout: 10 * time.Second},
		sseHTTP: &http.Client{},
	}
}

func (c *HTTPClient) authedGET(ctx context.Context, hc *http.Client, url string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDaemonUnreachable, err)
	}
	return resp, nil
}

func (c *HTTPClient) Sessions(ctx context.Context) ([]SessionSummary, error) {
	resp, err := c.authedGET(ctx, c.http, c.addr+"/v1/sessions")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrDaemonRestarted
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sessions: status %d", resp.StatusCode)
	}
	var ss []SessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&ss); err != nil {
		return nil, fmt.Errorf("sessions: decode: %w", err)
	}
	return ss, nil
}

func (c *HTTPClient) Graph(ctx context.Context, hash string) ([]SseEvent, error) {
	url := c.addr + "/v1/sessions/" + hash + "/graph"
	resp, err := c.authedGET(ctx, c.http, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrDaemonRestarted
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrSessionNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph: status %d", resp.StatusCode)
	}
	var evs []SseEvent
	if err := json.NewDecoder(resp.Body).Decode(&evs); err != nil {
		return nil, fmt.Errorf("graph: decode: %w", err)
	}
	return evs, nil
}

func (c *HTTPClient) Subscribe(ctx context.Context, hash string, sinceRev uint64) (<-chan StreamMsg, error) {
	url := c.addr + "/v1/subscribe?session=" + hash
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if sinceRev > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(sinceRev, 10))
	}
	resp, err := c.sseHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDaemonUnreachable, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, ErrDaemonRestarted
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("subscribe: status %d", resp.StatusCode)
	}
	ch := make(chan StreamMsg, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var ev SseEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				ch <- StreamMsg{Err: err}
				return
			}
			ch <- StreamMsg{Event: ev}
		}
		if err := sc.Err(); err != nil {
			ch <- StreamMsg{Err: err}
			return
		}
		ch <- StreamMsg{Done: true}
	}()
	return ch, nil
}

func (c *HTTPClient) Payload(ctx context.Context, hash, nodeID string) (PayloadView, error) {
	url := c.addr + "/v1/sessions/" + hash + "/nodes/" + nodeID + "/payload"
	resp, err := c.authedGET(ctx, c.http, url)
	if err != nil {
		return PayloadView{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return PayloadView{}, ErrDaemonRestarted
	}
	if resp.StatusCode == http.StatusForbidden {
		return PayloadView{}, ErrContentDisabled
	}
	if resp.StatusCode == http.StatusNotFound {
		return PayloadView{}, ErrPayloadNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PayloadView{}, fmt.Errorf("payload: status %d", resp.StatusCode)
	}
	var view PayloadView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return PayloadView{}, fmt.Errorf("payload: decode: %w", err)
	}
	return view, nil
}
