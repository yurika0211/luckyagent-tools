package tool

import (
	"context"
	"fmt"
	"strings"
)

// PendingApprovals aggregates optional external coding-agent bridges.
// Current Codex and Grok services resolve approvals through their own tools,
// so this list stays empty until those bridges expose a snapshot again.
func (s *Services) PendingApprovals() []PendingApproval {
	if s == nil {
		return nil
	}
	return nil
}

// RespondApproval resolves a pending approval using the provider-neutral API.
func (s *Services) RespondApproval(ctx context.Context, provider, approvalID, decision string) (PendingApproval, error) {
	if s == nil {
		return PendingApproval{}, fmt.Errorf("approval services are unavailable")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "codex":
		if s.Codex == nil {
			return PendingApproval{}, fmt.Errorf("codex approval service is disabled")
		}
		if _, err := s.Codex.respondApproval(map[string]any{
			"approval_id": approvalID,
			"decision":    decision,
		}); err != nil {
			return PendingApproval{}, err
		}
		return PendingApproval{Provider: "codex", ID: approvalID}, nil
	case "grok":
		if s.Grok == nil {
			return PendingApproval{}, fmt.Errorf("grok approval service is disabled")
		}
		if _, err := s.Grok.respondApproval(map[string]any{
			"approval_id": approvalID,
			"decision":    decision,
		}); err != nil {
			return PendingApproval{}, err
		}
		return PendingApproval{Provider: "grok", ID: approvalID}, nil
	default:
		return PendingApproval{}, fmt.Errorf("unsupported approval provider %q", provider)
	}
}
