package tool

import (
	"strings"

	"github.com/yurika0211/luckyagent-tools/approval"
)

// ApprovalOption and PendingApproval retain the original public API.
type ApprovalOption = approval.Option
type PendingApproval = approval.Pending

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
