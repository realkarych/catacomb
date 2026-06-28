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
	seq := d.next()
	ann := model.Annotation{
		ExecutionID: execID,
		SourceKey:   sourceKey,
		StepKey:     n.StepKey,
		Owner:       owner,
		Key:         key,
		Value:       value,
		WriteSeq:    seq,
	}
	if err := d.store.UpsertAnnotation(ann); err != nil {
		return fmt.Errorf("daemon.Annotate: %w", err)
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
	for _, n := range g.Nodes {
		if model.NodeSourceKey(n.ID) == sourceKey {
			return n
		}
	}
	return nil
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
	byKey := map[string][]model.Annotation{}
	for _, a := range anns {
		byKey[a.SourceKey] = append(byKey[a.SourceKey], a)
	}
	for sourceKey, group := range byKey {
		n := nodeBySourceKey(g, sourceKey)
		if n == nil {
			continue
		}
		for _, a := range group {
			n.Annotations = model.SetAnnotation(n.Annotations, a.Owner, a.Key, a.Value)
		}
	}
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

func (d *Daemon) annotationsAllowed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.allowAnnotations
}

func (d *Daemon) resolveHandle(hash, nodeID string) (execID, sourceKey string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, eid := range d.executionsForSession(hash) {
		g := d.graphs[eid]
		if g == nil {
			continue
		}
		for _, n := range g.Nodes {
			if n.ID == nodeID {
				return eid, model.NodeSourceKey(nodeID), true
			}
		}
	}
	return "", "", false
}

func (d *Daemon) handleNodeAnnotate(w http.ResponseWriter, r *http.Request) {
	if !d.annotationsAllowed() {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	hash := r.PathValue("hash")
	nodeID := r.PathValue("nodeId")
	var req annotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	execID, sourceKey, ok := d.resolveHandle(hash, nodeID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	err := d.Annotate(execID, sourceKey, req.Owner, req.Key, req.Value)
	if errors.Is(err, ErrInvalidAnnotation) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
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
