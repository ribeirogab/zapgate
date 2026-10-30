package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ribeirogab/zapgate/internal/admin"
)

const (
	urlFlag          = "--url"
	contentFlag      = "--content"
	sinceFlag        = "--since"
	noWebhookMessage = "no webhook yet"
)

func webhooks(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{
		"add":           webhooksAdd,
		"list":          webhooksList,
		"enable":        webhooksEnable,
		"remove":        webhooksRemove,
		"test":          webhooksTest,
		"rotate-secret": webhooksRotateSecret,
		"redeliver":     webhooksRedeliver,
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

func takeContent(args []string) (string, []string, error) {
	var content string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if strings.SplitN(args[i], "=", 2)[0] != contentFlag {
			rest = append(rest, args[i])
			continue
		}
		value, next, err := flagValue(args, i)
		if err != nil {
			return "", nil, err
		}
		content, i = value, next
	}
	return content, rest, nil
}

func webhooksAdd(ctx context.Context, args []string, std IO) error {
	content, args, err := takeContent(args)
	if err != nil {
		return err
	}
	opts, err := parseWebhookOptions(args)
	if err != nil || opts.url == "" || opts.accounts == "" {
		return errUsage
	}
	req := opts.request()
	req.Content = content
	created, err := ownerClient(std).AddWebhook(ctx, req)
	if err != nil {
		return err
	}
	w := created.Webhook
	fmt.Fprintf(std.Stdout, "created the webhook %s (accounts: %s, events: %s, content: %s)\n", w.Name, listOrNone(w.Accounts), strings.Join(w.Events, ","), w.Content)
	printSecretOnce(std, created.Secret)
	return nil
}

func printSecretOnce(std IO, secret string) {
	fmt.Fprintf(std.Stdout, "secret: %s\n", secret)
	fmt.Fprintln(std.Stdout, "this is the only time the secret is shown; store it now")
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
		rows = append(rows, []string{w.Name, w.URL, listOrDash(w.Accounts), w.Content, w.State,
			orDash(w.LastSuccess), strconv.Itoa(w.Pending), strconv.Itoa(w.Failed), deliveryErrorLabel(w.LastError)})
	}
	return writeTable(std.Stdout, []string{"NAME", "URL", "ACCOUNTS", "CONTENT", "STATE", "LAST SUCCESS", "PENDING", "FAILED", "LAST ERROR"}, rows)
}

func deliveryErrorLabel(e *admin.DeliveryError) string {
	switch {
	case e == nil:
		return noValue
	case e.Error != "":
		return e.Error
	}
	return statusLine(e.Status)
}

func webhooksEnable(ctx context.Context, args []string, std IO) error {
	if len(args) != 1 {
		return errUsage
	}
	enabled, err := ownerClient(std).EnableWebhook(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "enabled the webhook %s\n", enabled.Name)
	return nil
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

func webhooksRotateSecret(ctx context.Context, args []string, std IO) error {
	if len(args) != 1 {
		return errUsage
	}
	rotated, err := ownerClient(std).RotateWebhookSecret(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "rotated the secret of the webhook %s; the old secret also signs until %s\n", rotated.Name, rotated.PreviousSecretUntil)
	printSecretOnce(std, rotated.Secret)
	return nil
}

func webhooksRedeliver(ctx context.Context, args []string, std IO) error {
	var since string
	name, err := parseNamedArgs(args, map[string]*string{sinceFlag: &since})
	if err != nil {
		return err
	}
	redelivered, err := ownerClient(std).RedeliverWebhook(ctx, name, admin.RedeliverRequest{Since: since})
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "opened again %s of the webhook %s\n", failedDeliveries(redelivered.Deliveries), redelivered.Name)
	return nil
}

func failedDeliveries(count int) string {
	if count == 1 {
		return "1 failed delivery"
	}
	return fmt.Sprintf("%d failed deliveries", count)
}

func statusLine(status int) string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", status, http.StatusText(status)))
}
