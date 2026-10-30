package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	programName  = "zapgate"
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	usageMessage = `usage:
  zapgate serve
  zapgate health
  zapgate status
  zapgate accounts add <name> --phone <number> [--description <text>] [--qr]
  zapgate accounts describe <name> <text>
  zapgate accounts list
  zapgate accounts remove <name> [--yes]
  zapgate accounts retention <name> <months|default>
  zapgate tokens create <name> --accounts <a,b|all> [--read-only] [--approval elicitation|app]
  zapgate access list
  zapgate access set <name> [--accounts <a,b|all>] [--approval elicitation|app]
  zapgate access revoke <name>
  zapgate webhooks add <name> --url <url> --accounts <a,b|all>
      [--events message.received,account.state] [--chats all|direct|groups|<chat,...>]
      [--content minimal|full] [--quiet 5s] [--max-wait 30s]
  zapgate webhooks list
  zapgate webhooks set <name> [the options of add]
  zapgate webhooks enable <name>
  zapgate webhooks remove <name>
  zapgate webhooks test <name>
  zapgate webhooks rotate-secret <name>
  zapgate webhooks redeliver <name> [--since <duration>]
  zapgate password
`
)

type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
}

func System() IO {
	return IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv}
}

var errUsage = errors.New("usage")

type command func(ctx context.Context, args []string, std IO) error

func commands() map[string]command {
	return map[string]command{
		"serve":    serve,
		"health":   health,
		"status":   status,
		"accounts": accounts,
		"tokens":   tokens,
		"access":   access,
		"webhooks": webhooks,
		"password": password,
	}
}

func Run(ctx context.Context, args []string, std IO) int {
	if len(args) == 0 {
		return usage(std)
	}
	cmd, ok := commands()[args[0]]
	if !ok {
		return usage(std)
	}
	err := cmd(ctx, args[1:], std)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		return usage(std)
	default:
		fmt.Fprintf(std.Stderr, "%s: %v\n", programName, err)
		return exitFailure
	}
}

func usage(std IO) int {
	fmt.Fprint(std.Stderr, usageMessage)
	return exitUsage
}

func subcommand(args []string, table map[string]command) (command, []string, error) {
	if len(args) == 0 {
		return nil, nil, errUsage
	}
	cmd, ok := table[args[0]]
	if !ok {
		return nil, nil, errUsage
	}
	return cmd, args[1:], nil
}

func accounts(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{
		"add":       accountsAdd,
		"describe":  accountsDescribe,
		"list":      accountsList,
		"remove":    accountsRemove,
		"retention": accountsRetention,
	})
	if err != nil {
		return err
	}
	return cmd(ctx, rest, std)
}

func tokens(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{"create": tokensCreate})
	if err != nil {
		return err
	}
	return cmd(ctx, rest, std)
}

func access(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{"list": accessList, "set": accessSet, "revoke": accessRevoke})
	if err != nil {
		return err
	}
	return cmd(ctx, rest, std)
}
