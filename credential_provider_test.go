package tool

import (
	"context"
	"fmt"
)

type testCredentialProvider struct{}

func (testCredentialProvider) WithCredential(ctx context.Context, ref string, use func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref != "github-main" {
		return fmt.Errorf("unknown credential %s", ref)
	}
	secret := []byte("super-secret")
	defer clear(secret)
	return use(secret)
}
