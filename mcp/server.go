package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
)

var Version = "dev"

const (
	defaultProtocolVersion = "2025-06-18"
	codeParse              = -32700
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		if ctx.Err() != nil {
			return nil
		}
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if werr := handleLine(trimmed, w); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func handleLine(line []byte, w io.Writer) error {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return writeMessage(w, errorResponse(json.RawMessage("null"), codeParse, "parse error"))
	}
	if len(req.ID) == 0 {
		return nil
	}
	result, rpcErr := dispatch(req)
	if rpcErr != nil {
		return writeMessage(w, errorResponse(req.ID, rpcErr.Code, rpcErr.Message))
	}
	return writeMessage(w, resultResponse(req.ID, result))
}

func dispatch(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return initializeResult(req.Params), nil
	case "tools/list":
		return toolsListResult(), nil
	case "tools/call":
		return toolsCallResult(req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func initializeResult(params json.RawMessage) map[string]any {
	version := defaultProtocolVersion
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "catacomb", "version": Version},
	}
}

func toolsListResult() map[string]any {
	return map[string]any{"tools": []any{markToolSpec()}}
}

func markToolSpec() map[string]any {
	return map[string]any{
		"name":        "mark",
		"description": "Record a phase-boundary checkpoint in the current catacomb session. Call at the start and end of each named phase (for example impl, verify) so regress and diff can scope to it.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":       map[string]any{"type": "string", "description": "phase label, for example impl or verify"},
				"boundary":   map[string]any{"type": "string", "enum": []any{"start", "end"}, "description": "start or end of the phase"},
				"occurrence": map[string]any{"type": "integer", "description": "explicit occurrence ordinal for repeated same-name phases (optional)"},
				"state_ref":  map[string]any{"type": "string", "description": "opaque state reference (optional)"},
			},
			"required": []any{"name", "boundary"},
		},
	}
}

func toolsCallResult(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params"}
	}
	if p.Name != "mark" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	var a struct {
		Name     string `json:"name"`
		Boundary string `json:"boundary"`
	}
	if err := json.Unmarshal(p.Arguments, &a); err != nil {
		return toolResult("mark: invalid arguments", true), nil
	}
	if a.Name == "" || (a.Boundary != "start" && a.Boundary != "end") {
		return toolResult("mark: name is required and boundary must be start or end", true), nil
	}
	return toolResult("marked "+a.Boundary+" "+a.Name, false), nil
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
}

func writeMessage(w io.Writer, v any) error {
	b, _ := json.Marshal(v)
	_, err := w.Write(append(b, '\n'))
	return err
}

func errorResponse(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func resultResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}
