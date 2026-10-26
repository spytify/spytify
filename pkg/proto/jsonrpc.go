package proto

import (
	"encoding/json"
	"fmt"
)

const JSONRPCVersion = "2.0"

// Message is any JSON-RPC 2.0 object on the wire. A request has a method and
// an id, a notification has a method and no id, a response has an id and
// either a result or an error.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (m *Message) IsRequest() bool      { return m.Method != "" && m.ID != nil }
func (m *Message) IsNotification() bool { return m.Method != "" && m.ID == nil }
func (m *Message) IsResponse() bool     { return m.Method == "" && m.ID != nil }

// Standard JSON-RPC error codes, plus the application range used by the core.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeTargetNotRunning = -32001
	CodeFFmpegMissing    = -32002
	CodeSessionActive    = -32003
	CodeNoSession        = -32004
	CodeUnsupported      = -32005
	CodeNotAvailable     = -32006
	CodeOutputDir        = -32007
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// Errorf builds an *Error with a formatted message.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
