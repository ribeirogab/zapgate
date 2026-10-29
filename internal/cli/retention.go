package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ribeirogab/zapgate/internal/config"
)

const defaultRetention = "default"

func accountsRetention(ctx context.Context, args []string, std IO) error {
	if len(args) != 2 {
		return errUsage
	}
	months, err := parseRetention(args[1])
	if err != nil {
		return err
	}
	account, err := ownerClient(std).SetRetention(ctx, args[0], months)
	if err != nil {
		return err
	}
	kept := monthsLabel(account.RetentionMonths)
	if account.RetentionDefault {
		kept = "the default of " + kept
	}
	fmt.Fprintf(std.Stdout, "the account %s keeps %s of messages; the next daily pass deletes the older ones\n", account.Name, kept)
	return nil
}

func parseRetention(value string) (int, error) {
	if value == defaultRetention {
		return 0, nil
	}
	months, err := strconv.Atoi(value)
	if err != nil || months < config.MinRetentionMonths {
		return 0, errUsage
	}
	return months, nil
}
