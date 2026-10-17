// Package approval defines provider-neutral permission request values.
package approval

import "time"

// Option is a choice accepted by the owner of a permission request.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
}

// Pending is the client-facing view of an outstanding permission request.
type Pending struct {
	Provider  string         `json:"provider"`
	ID        string         `json:"id"`
	Method    string         `json:"method,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	TurnID    string         `json:"turn_id,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
	Options   []Option       `json:"options"`
	CreatedAt time.Time      `json:"created_at"`
}
