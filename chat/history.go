package chat

import "time"

// GroupMessage is the privacy-minimized representation of a OneBot group
// history item. It deliberately does not expose the sender's QQ number.
type GroupMessage struct {
	MessageID string    `json:"message_id"`
	UserName  string    `json:"user_name"`
	Content   string    `json:"content"`
	Time      time.Time `json:"time"`
}

// GroupInfo is the small subset of OneBot group metadata needed to resolve a
// group target and label a history result.
type GroupInfo struct {
	GroupID     string `json:"group_id"`
	Name        string `json:"group_name"`
	MemberCount int    `json:"member_count,omitempty"`
}
