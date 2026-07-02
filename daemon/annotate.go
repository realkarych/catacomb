package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

var (
	ErrAnnotationsDisabled = errors.New("daemon: annotations disabled")
	ErrInvalidAnnotation   = errors.New("daemon: invalid annotation")
	ErrAnnotationTarget    = errors.New("daemon: annotation target not found")
)

func (d *Daemon) SetAllowAnnotations(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.allowAnnotations = v
}

func (d *Daemon) Annotate(execID, sourceKey, owner, key string, value json.RawMessage) error {
	if err := validateAnnotation(owner, key, value); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.allowAnnotations {
		return fmt.Errorf("daemon.Annotate: %w", ErrAnnotationsDisabled)
	}
	g := d.graphs[execID]
	if g == nil {
		return fmt.Errorf("daemon.Annotate: %w", ErrAnnotationTarget)
	}
	n := nodeBySourceKey(g, sourceKey)
	if n == nil {
		return fmt.Errorf("daemon.Annotate: %w", ErrAnnotationTarget)
	}
	if err := d.annotateNodeLocked(execID, n, owner, key, value); err != nil {
		return fmt.Errorf("daemon.Annotate: %w", err)
	}
	return nil
}

func (d *Daemon) annotateNodeLocked(execID string, n *model.Node, owner, key string, value json.RawMessage) error {
	seq := d.next()
	ann := model.Annotation{
		ExecutionID: execID,
		SourceKey:   model.NodeSourceKey(n.ID),
		StepKey:     n.StepKey,
		Owner:       owner,
		Key:         key,
		Value:       value,
		WriteSeq:    seq,
	}
	if err := d.store.UpsertAnnotation(ann); err != nil {
		return err
	}
	n.Annotations = model.SetAnnotation(n.Annotations, owner, key, value)
	if seq > n.Rev {
		n.Rev = seq
	}
	d.publishDelta(cdc.GraphDelta{
		Kind:        cdc.DeltaNodeUpsert,
		Rev:         n.Rev,
		Node:        n,
		RunID:       n.RunID,
		ExecutionID: execID,
	})
	return nil
}

func validateAnnotation(owner, key string, value json.RawMessage) error {
	if owner == "" || strings.Contains(owner, ".") {
		return fmt.Errorf("daemon.Annotate: %w", ErrInvalidAnnotation)
	}
	if key == "" || strings.Contains(key, ".") {
		return fmt.Errorf("daemon.Annotate: %w", ErrInvalidAnnotation)
	}
	if !json.Valid(value) {
		return fmt.Errorf("daemon.Annotate: %w", ErrInvalidAnnotation)
	}
	return nil
}

func nodeBySourceKey(g *reduce.Graph, sourceKey string) *model.Node {
	return g.NodeBySourceKey(sourceKey)
}

func reattachAnnotations(d *Daemon) error {
	for execID, g := range d.graphs {
		anns, err := d.store.AnnotationsForExecution(execID)
		if err != nil {
			return fmt.Errorf("daemon.reattachAnnotations: %w", err)
		}
		applyAnnotations(g, anns)
	}
	return nil
}

func applyAnnotations(g *reduce.Graph, anns []model.Annotation) {
	g.ApplyAnnotations(anns)
}

func (d *Daemon) carryOverMergeLocked(execID, oldID, newID string) {
	fromKey := model.NodeSourceKey(oldID)
	toKey := model.NodeSourceKey(newID)
	if fromKey == toKey || fromKey == "" || toKey == "" {
		return
	}
	if err := d.store.MoveAnnotations(execID, fromKey, toKey); err != nil {
		return
	}
	g := d.graphs[execID]
	if g == nil {
		return
	}
	anns, err := d.store.AnnotationsForExecution(execID)
	if err != nil {
		return
	}
	applyAnnotations(g, anns)
	if oldNode := nodeBySourceKey(g, fromKey); oldNode != nil {
		oldNode.Annotations = nil
	}
	n := nodeBySourceKey(g, toKey)
	if n == nil {
		return
	}
	d.publishDelta(cdc.GraphDelta{
		Kind:        cdc.DeltaNodeUpsert,
		Rev:         n.Rev,
		Node:        n,
		RunID:       n.RunID,
		ExecutionID: execID,
	})
}

type annotateRequest struct {
	Owner string          `json:"owner"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func (d *Daemon) resolveAndAnnotateLocked(hash, nodeID string, req annotateRequest) error {
	if !d.allowAnnotations {
		return fmt.Errorf("daemon.handleNodeAnnotate: %w", ErrAnnotationsDisabled)
	}
	for _, eid := range d.executionsForSession(hash) {
		g := d.graphs[eid]
		for _, n := range g.Nodes {
			if n.ID == nodeID {
				if err := d.annotateNodeLocked(eid, n, req.Owner, req.Key, req.Value); err != nil {
					return fmt.Errorf("daemon.handleNodeAnnotate: %w", err)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("daemon.handleNodeAnnotate: %w", ErrAnnotationTarget)
}

func (d *Daemon) handleNodeAnnotate(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	nodeID := r.PathValue("nodeId")
	var req annotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validateAnnotation(req.Owner, req.Key, req.Value); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	err := d.resolveAndAnnotateLocked(hash, nodeID, req)
	d.mu.Unlock()
	if errors.Is(err, ErrAnnotationsDisabled) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if errors.Is(err, ErrAnnotationTarget) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
