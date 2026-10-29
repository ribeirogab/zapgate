package cli

import (
	"context"
	"fmt"
)

func password(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	secret, err := ownerClient(std).Password(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(std.Stdout, secret)
	return nil
}
