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
	chatsFlag        = "--chats"
	eventsFlag       = "--events"
	contentFlag      = "--content"
	noWebhookMessage = "no webhook yet"
)

func webhooks(ctx context.Context, args []string, std IO) error {
	cmd, rest, err := subcommand(args, map[string]command{
		"add":           webhooksAdd,
		"list":          webhooksList,
		"set":           webhooksSet,
		"remove":        webhooksRemove,
		"test":          webhooksTest,
		"rotate-secret": webhooksRotateSecret,
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
	chats    string
	events   string
	content  string
}

func (o *webhookOptions) flags() map[string]*string {
	return map[string]*string{
		urlFlag:      &o.url,
		accountsFlag: &o.accounts,
		chatsFlag:    &o.chats,
		eventsFlag:   &o.events,
		contentFlag:  &o.content,
	}
}

func (o webhookOptions) given() bool {
	for _, value := range o.flags() {
		if *value != "" {
			return true
		}
	}
	return false
}

func parseWebhookOptions(args []string) (webhookOptions, error) {
	var opts webhookOptions
	name, err := parseNamedArgs(args, opts.flags())
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

func (o webhookOptions) options() admin.WebhookOptions {
	options := admin.WebhookOptions{URL: o.url, Chats: o.chats, Events: commaList(o.events), Content: o.content}
	if o.accounts != "" {
		options.AllExistingAccounts, options.Accounts = accountChoice(o.accounts)
	}
	return options
}

func commaList(value string) []string {
	if value == "" {
		return nil
	}
	items := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func webhooksAdd(ctx context.Context, args []string, std IO) error {
	opts, err := parseWebhookOptions(args)
	if err != nil || opts.url == "" || opts.accounts == "" {
		return errUsage
	}
	created, err := ownerClient(std).AddWebhook(ctx, admin.AddWebhookRequest{Name: opts.name, WebhookOptions: opts.options()})
	if err != nil {
		return err
	}
	w := created.Webhook
	fmt.Fprintf(std.Stdout, "created the webhook %s (accounts: %s, events: %s, content: %s)\n", w.Name, listOrNone(w.Accounts), strings.Join(w.Events, ","), w.Content)
	printSecretOnce(std, created.Secret)
	return nil
}

func webhooksSet(ctx context.Context, args []string, std IO) error {
	opts, err := parseWebhookOptions(args)
	if err != nil || !opts.given() {
		return errUsage
	}
	w, err := ownerClient(std).SetWebhook(ctx, opts.name, opts.options())
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "changed the webhook %s (accounts: %s, chats: %s, events: %s, content: %s)\n", w.Name, listOrNone(w.Accounts), chatsLabel(w), strings.Join(w.Events, ","), w.Content)
	return nil
}

func chatsLabel(w admin.WebhookView) string {
	if len(w.ListedChats) > 0 {
		return strings.Join(w.ListedChats, ",")
	}
	return w.Chats
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
	header := make([]string, 0, len(webhookColumns))
	for _, column := range webhookColumns {
		header = append(header, column.header)
	}
	rows := make([][]string, 0, len(list))
	for _, w := range list {
		row := make([]string, 0, len(webhookColumns))
		for _, column := range webhookColumns {
			row = append(row, column.value(w))
		}
		rows = append(rows, row)
	}
	return writeTable(std.Stdout, header, rows)
}

type webhookColumn struct {
	header string
	value  func(admin.WebhookView) string
}

var webhookColumns = []webhookColumn{
	{header: "NAME", value: func(w admin.WebhookView) string { return w.Name }},
	{header: "URL", value: func(w admin.WebhookView) string { return w.URL }},
	{header: "ACCOUNTS", value: func(w admin.WebhookView) string { return listOrDash(w.Accounts) }},
	{header: "CHATS", value: chatsLabel},
	{header: "EVENTS", value: func(w admin.WebhookView) string { return strings.Join(w.Events, ",") }},
	{header: "CONTENT", value: func(w admin.WebhookView) string { return w.Content }},
	{header: "STATE", value: func(w admin.WebhookView) string { return w.State }},
	{header: "LAST SUCCESS", value: func(w admin.WebhookView) string { return orDash(w.LastSuccess) }},
	{header: "PENDING", value: func(w admin.WebhookView) string { return strconv.Itoa(w.Pending) }},
	{header: "FAILED", value: func(w admin.WebhookView) string { return strconv.Itoa(w.Failed) }},
	{header: "LAST ERROR", value: func(w admin.WebhookView) string { return deliveryErrorLabel(w.LastError) }},
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

func statusLine(status int) string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", status, http.StatusText(status)))
}
