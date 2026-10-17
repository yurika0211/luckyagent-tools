package tool

import "context"

// CredentialProvider grants access for one call; the host owns storage and cleanup.
type CredentialProvider interface {
	WithCredential(context.Context, string, func([]byte) error) error
}
