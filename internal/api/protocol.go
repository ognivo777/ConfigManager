// Package api implements the JSON-over-Unix-socket IPC protocol between the
// cm CLI and the cm daemon.
package api

import (
	"encoding/json"
	"fmt"
)

// Version is the API protocol version.
const Version = 1

// Request is a single JSON request from CLI to daemon.
type Request struct {
	Version int             `json:"version"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a single JSON response from daemon to CLI.
type Response struct {
	Version int             `json:"version"`
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// Method names.
const (
	MAdd     = "add"
	MHistory = "history"
	MDiff    = "diff"
	MMessage = "message"
	MRestore = "restore"
	MPreview = "previewRestore"
	MList    = "list"
	MStop    = "stop"
)

func ErrResponse(err error) *Response {
	return &Response{Version: Version, OK: false, Error: err.Error()}
}

func OkResponse(result any) *Response {
	b, err := json.Marshal(result)
	if err != nil {
		return ErrResponse(fmt.Errorf("failed to encode response: %w", err))
	}
	return &Response{Version: Version, OK: true, Result: b}
}
