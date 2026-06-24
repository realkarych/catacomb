package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type detailState struct {
	node       *Node
	payload    PayloadView
	revealed   bool
	disabled   bool
	payloadErr error
}

func newDetailState() detailState {
	return detailState{}
}

func (d detailState) withNode(n Node) detailState {
	d.node = &n
	d.revealed = false
	d.disabled = false
	d.payloadErr = nil
	d.payload = PayloadView{}
	return d
}

func (d detailState) update(msg tea.Msg, client Client, hash string) (detailState, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if m.Type == tea.KeyRunes && string(m.Runes) == "c" {
			if d.node == nil {
				return d, nil
			}
			return d, loadPayloadCmd(client, hash, d.node.ID)
		}
	case payloadLoadedMsg:
		if errors.Is(m.err, ErrContentDisabled) {
			d.disabled = true
			return d, nil
		}
		if m.err != nil {
			d.payloadErr = m.err
			return d, nil
		}
		d.payload = m.view
		d.revealed = true
		return d, nil
	}
	return d, nil
}

func (d detailState) view(s Styles, debug bool) string {
	if d.node == nil {
		return s.Faint.Render("select a node to view details")
	}
	n := d.node
	var b strings.Builder

	typeLabel := NodeTypeLabel(n.Type)
	nameStr := n.Name
	if nameStr == "" {
		nameStr = typeLabel
	}
	fmt.Fprintf(&b, "%s  %s\n\n", s.Bold.Render(nameStr), s.Faint.Render(typeLabel))

	statusGlyph := StatusGlyph(n.Status)
	statusLabel := StatusLabel(n.Status)
	fmt.Fprintf(&b, "status    %s %s\n", statusGlyph, statusLabel)
	fmt.Fprintf(&b, "duration  %s\n", Duration(n.DurationMS))

	tIn := "—"
	tOut := "—"
	if n.TokensIn != nil {
		tIn = Tokens(n.TokensIn)
	}
	if n.TokensOut != nil {
		tOut = Tokens(n.TokensOut)
	}
	fmt.Fprintf(&b, "tokens    %s → %s\n", tIn, tOut)

	prov := Provenance(*n)
	fmt.Fprintf(&b, "cost      %s  (%s)\n", Cost(n.CostUSD), prov)

	model := "—"
	if n.Attrs != nil {
		if v, ok := n.Attrs["model"]; ok {
			if s2, ok2 := v.(string); ok2 && s2 != "" {
				model = s2
			}
		}
	}
	fmt.Fprintf(&b, "model     %s\n", model)

	if debug {
		b.WriteString("\n")
		fmt.Fprintf(&b, "id        %s\n", n.ID)
		if n.PayloadHash != "" {
			fmt.Fprintf(&b, "hash      %s\n", ShortHash(n.PayloadHash, 12))
		}
		for _, src := range n.Sources {
			fmt.Fprintf(&b, "source    %s / %s\n", src.Source, src.ObsID)
		}
	}

	b.WriteString("\n")
	b.WriteString(renderContentSection(d, s))

	return b.String()
}

func renderContentSection(d detailState, s Styles) string {
	if d.disabled {
		return s.Faint.Render("content viewing disabled by the daemon (start it with --allow-payload-access)")
	}
	if d.payloadErr != nil {
		return s.Faint.Render(fmt.Sprintf("content error: %v", d.payloadErr))
	}
	if !d.revealed {
		return s.Faint.Render("press c to view content")
	}
	var b strings.Builder
	if d.payload.Redacted {
		b.WriteString("⚠ redacted fields: ")
		for i, r := range d.payload.Redactions {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(r.Path)
		}
		b.WriteString("\n\n")
	}
	if len(d.payload.Input) > 0 {
		b.WriteString("input:\n")
		b.WriteString(prettyJSON(d.payload.Input))
		b.WriteString("\n")
	}
	if len(d.payload.Output) > 0 {
		b.WriteString("output:\n")
		b.WriteString(prettyJSON(d.payload.Output))
		b.WriteString("\n")
	}
	return b.String()
}

func prettyJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
