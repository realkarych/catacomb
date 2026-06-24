package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/realkarych/catacomb/redact"
)

var (
	ErrPayloadAccessDisabled = errors.New("daemon: payload access disabled")
	ErrPayloadNotFound       = errors.New("daemon: payload not found")
)

type RedactionFinding struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PayloadView struct {
	NodeID      string             `json:"node_id"`
	PayloadHash string             `json:"payload_hash,omitempty"`
	Input       json.RawMessage    `json:"input,omitempty"`
	Output      json.RawMessage    `json:"output,omitempty"`
	Redactions  []RedactionFinding `json:"redactions"`
	Redacted    bool               `json:"redacted"`
}

func (d *Daemon) nodePayloadView(hash, selector string) (PayloadView, error) {
	if !d.allowPayloadAccess {
		return PayloadView{}, fmt.Errorf("daemon.nodePayloadView: %w", ErrPayloadAccessDisabled)
	}
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return PayloadView{}, fmt.Errorf("daemon.nodePayloadView: %w", ErrSessionNotFound)
	}
	for _, execID := range execs {
		g := d.graphs[execID]
		for _, n := range g.Nodes {
			if n.ID == selector || n.PayloadHash == selector {
				if n.Payload == nil || (len(n.Payload.Input) == 0 && len(n.Payload.Output) == 0) {
					return PayloadView{}, fmt.Errorf("daemon.nodePayloadView: %w", ErrPayloadNotFound)
				}
				rawIn := n.Payload.Input
				rawOut := n.Payload.Output
				payloadHash := n.PayloadHash
				nodeID := n.ID
				d.mu.Unlock()
				view := buildPayloadView(nodeID, payloadHash, rawIn, rawOut)
				d.mu.Lock()
				return view, nil
			}
		}
	}
	return PayloadView{}, fmt.Errorf("daemon.nodePayloadView: %w", ErrPayloadNotFound)
}

func buildPayloadView(nodeID, payloadHash string, rawIn, rawOut json.RawMessage) PayloadView {
	var allFindings []RedactionFinding
	var redactedIn, redactedOut json.RawMessage
	if len(rawIn) > 0 {
		rIn := redact.Redact(rawIn)
		redactedIn = json.RawMessage(rIn.Data)
		for _, f := range rIn.Findings {
			allFindings = append(allFindings, RedactionFinding{Path: f.Path, Reason: f.Reason})
		}
	}
	if len(rawOut) > 0 {
		rOut := redact.Redact(rawOut)
		redactedOut = json.RawMessage(rOut.Data)
		for _, f := range rOut.Findings {
			allFindings = append(allFindings, RedactionFinding{Path: f.Path, Reason: f.Reason})
		}
	}
	if allFindings == nil {
		allFindings = []RedactionFinding{}
	}
	return PayloadView{
		NodeID:      nodeID,
		PayloadHash: payloadHash,
		Input:       redactedIn,
		Output:      redactedOut,
		Redactions:  allFindings,
		Redacted:    len(allFindings) > 0,
	}
}

func (d *Daemon) handleNodePayload(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	nodeID := r.PathValue("nodeId")
	d.mu.Lock()
	view, err := d.nodePayloadView(hash, nodeID)
	d.mu.Unlock()
	if errors.Is(err, ErrPayloadAccessDisabled) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}
