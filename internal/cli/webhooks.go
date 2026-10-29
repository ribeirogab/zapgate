package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ribeirogab/zapgate/internal/admin"
)

const (
	urlFlag          = "--url"
	noWebhookMessage = "no webhook yet"
)

func webhooks(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{
		"add":    webhooksAdd,
		"list":   webhooksList,
		"remove": webhooksRemove,
		"test":   webhooksTest,
	})
	if err != nil {
		return err
	}
	return cmd(ctx, rest, std)
}

type webhookOptions struct {
	name     string
	url      string
	accounts string
}

func parseWebhookOptions(args []string) (webhookOptions, error) {
	var opts webhookOptions
	name, err := parseNamedArgs(args, map[string]*string{urlFlag: &opts.url, accountsFlag: &opts.accounts})
	opts.name = name
	return opts, err
}

func parseNamedArgs(args []string, values map[string]*string) (string, error) {
	var name string
	for i := 0; i < len(args); i++ {
		if target, ok := values[strings.SplitN(args[i], "=", 2)[0]]; ok {
			value, next, err := flagValue(args, i)
			if err != nil {
				return "", err
			}
			*target, i = value, next
			continue
		}
		if strings.HasPrefix(args[i], "-") || name != "" {
			return "", errUsage
		}
		name = args[i]
	}
	if name == "" {
		return "", errUsage
	}
	return name, nil
}

func (o webhookOptions) request() admin.AddWebhookRequest {
	all, names := accountChoice(o.accounts)
	return admin.AddWebhookRequest{Name: o.name, URL: o.url, AllExistingAccounts: all, Accounts: names}
}

func webhooksAdd(ctx context.Context, args []string, std IO) error {
	opts, err := parseWebhookOptions(args)
	if err != nil || opts.url == "" || opts.accounts == "" {
		return errUsage
	}
	created, err := ownerClient(std).AddWebhook(ctx, opts.request())
	if err != nil {
		return err
	}
	w := created.Webhook
	fmt.Fprintf(std.Stdout, "created the webhook %s (accounts: %s, events: %s, content: %s)\n", w.Name, listOrNone(w.Accounts), strings.Join(w.Events, ","), w.Content)
	fmt.Fprintf(std.Stdout, "secret: %s\n", created.Secret)
	fmt.Fprintln(std.Stdout, "this is the only time the secret is shown; store it now")
	return nil
}

func webhooksList(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	list, err := ownerClient(std).Webhooks(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(std.Stdout, noWebhookMessage)
		return nil
	}
	rows := make([][]string, 0, len(list))
	for _, w := range list {
		rows = append(rows, []string{w.Name, w.URL, listOrDash(w.Accounts), w.State})
	}
	return writeTable(std.Stdout, []string{"NAME", "URL", "ACCOUNTS", "STATE"}, rows)
}

func webhooksRemove(ctx context.Context, args []string, std IO) error {
	if len(args) != 1 {
		return errUsage
	}
	removed, err := ownerClient(std).RemoveWebhook(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "removed the webhook %s\n", removed.Name)
	return nil
}

func webhooksTest(ctx context.Context, args []string, std IO) error {
	if len(args) != 1 {
		return errUsage
	}
	tested, err := ownerClient(std).TestWebhook(ctx, args[0])
	if err != nil {
		return err
	}
	if tested.Error != "" {
		return fmt.Errorf("the webhook %s did not reach its receiver: %s", tested.Name, tested.Error)
	}
	answer := fmt.Sprintf("the receiver of the webhook %s answered %s", tested.Name, statusLine(tested.Status))
	if tested.Status < http.StatusOK || tested.Status >= http.StatusMultipleChoices {
		return fmt.Errorf("%s", answer)
	}
	fmt.Fprintln(std.Stdout, answer)
	return nil
}

func statusLine(status int) string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", status, http.StatusText(status)))
}
