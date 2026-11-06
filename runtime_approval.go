package tool

import (
	"context"
	"fmt"
	"strings"
)

// PendingApprovals returns approvals this service layer can snapshot.
// No external coding-agent bridge is registered, so the list stays empty.
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
	_ = ctx
	_ = approvalID
	_ = decision
	return PendingApproval{}, fmt.Errorf("unsupported approval provider %q", provider)
}
