package tool

import (
	"strings"
	"time"
)

// ApprovalOption is the client-facing choice for a pending external-agent
// permission request. ID is the exact value accepted by the provider bridge.
type ApprovalOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
}

// PendingApproval is deliberately provider-neutral so HTTP and mobile clients
// do not need to know the Codex JSON-RPC or Grok ACP wire formats.
type PendingApproval struct {
	Provider  string           `json:"provider"`
	ID        string           `json:"id"`
	Method    string           `json:"method,omitempty"`
	ThreadID  string           `json:"thread_id,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	TurnID    string           `json:"turn_id,omitempty"`
	Reason    string           `json:"reason,omitempty"`
	Summary   string           `json:"summary,omitempty"`
	Params    map[string]any   `json:"params,omitempty"`
	Options   []ApprovalOption `json:"options"`
	CreatedAt time.Time        `json:"created_at"`
}

func approvalOptionLabel(kind, name, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "allow_once", "accept_once", "accept":
		return "允许一次"
	case "allow_always", "accept_for_session", "acceptforsession":
		return "本会话允许同类"
	case "reject_once", "deny", "decline", "reject":
		return "拒绝"
	case "cancel", "cancelled":
		return "取消"
	}
	if text := strings.TrimSpace(name); text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}
