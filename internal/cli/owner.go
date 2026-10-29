package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/config"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	allAccounts       = "all"
	accountsFlag      = "--accounts"
	approvalFlag      = "--approval"
	readOnlyFlag      = "--read-only"
	noAccountsMessage = "no accounts yet; connect one with: zapgate accounts add <name> --phone <number>"
	noAccessMessage   = "no access yet"
	modeReadOnly      = "read-only"
	modeReadWrite     = "read-write"
	noValue           = "-"
	noAccountsLabel   = "none"
)

func ownerClient(std IO) *admin.Client {
	return admin.Dial(config.AdminSocketFromEnv(std.Getenv))
}

func status(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	view, err := ownerClient(std).Status(ctx)
	if err != nil {
		return err
	}
	if len(view.Accounts) == 0 {
		fmt.Fprintln(std.Stdout, "accounts: none")
		return nil
	}
	if err := writeTable(std.Stdout, statusHeader, statusRows(view.Accounts)); err != nil {
		return err
	}
	fmt.Fprintln(std.Stdout, retentionLine(view.Retention))
	return nil
}

func accountsList(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	accounts, err := ownerClient(std).Accounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Fprintln(std.Stdout, noAccountsMessage)
		return nil
	}
	return writeTable(std.Stdout, []string{"NAME", "PHONE", "PROFILE", "STATE", "DESCRIPTION"}, accountRows(accounts))
}

func accountRows(accounts []admin.AccountView) [][]string {
	rows := make([][]string, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, []string{a.Name, a.Phone, orDash(a.ProfileName), a.State, a.Description})
	}
	return rows
}

type tokenOptions struct {
	name     string
	accounts string
	approval string
	readOnly bool
}

func parseTokenOptions(args []string) (tokenOptions, error) {
	var opts tokenOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == readOnlyFlag {
			opts.readOnly = true
			continue
		}
		if target := opts.valueOf(arg); target != nil {
			value, next, err := flagValue(args, i)
			if err != nil {
				return opts, err
			}
			*target, i = value, next
			continue
		}
		if strings.HasPrefix(arg, "-") || opts.name != "" {
			return opts, errUsage
		}
		opts.name = arg
	}
	if opts.name == "" || (opts.approval != "" && !store.ValidApproval(opts.approval)) {
		return opts, errUsage
	}
	return opts, nil
}

func (o *tokenOptions) valueOf(arg string) *string {
	switch strings.SplitN(arg, "=", 2)[0] {
	case accountsFlag:
		return &o.accounts
	case approvalFlag:
		return &o.approval
	}
	return nil
}

func flagValue(args []string, i int) (string, int, error) {
	if _, value, inline := strings.Cut(args[i], "="); inline {
		return nonEmpty(value, i)
	}
	if i+1 >= len(args) {
		return "", i, errUsage
	}
	return nonEmpty(args[i+1], i+1)
}

func nonEmpty(value string, i int) (string, int, error) {
	if value == "" {
		return "", i, errUsage
	}
	return value, i, nil
}

func (o tokenOptions) request() admin.CreateTokenRequest {
	all, names := accountChoice(o.accounts)
	return admin.CreateTokenRequest{Name: o.name, ReadOnly: o.readOnly, Approval: o.approval, AllExistingAccounts: all, Accounts: names}
}

func (o tokenOptions) change() admin.SetAccessRequest {
	req := admin.SetAccessRequest{Approval: o.approval}
	if o.accounts != "" {
		req.AllExistingAccounts, req.Accounts = accountChoice(o.accounts)
	}
	return req
}

func accountChoice(value string) (bool, []string) {
	if value == allAccounts {
		return true, nil
	}
	names := []string{}
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return false, names
}

func accessSet(ctx context.Context, args []string, std IO) error {
	opts, err := parseTokenOptions(args)
	if err != nil || opts.readOnly || (opts.accounts == "" && opts.approval == "") {
		return errUsage
	}
	changed, err := ownerClient(std).SetAccess(ctx, opts.name, opts.change())
	if err != nil {
		return err
	}
	if opts.accounts != "" {
		fmt.Fprintf(std.Stdout, "the access %s now reaches the accounts: %s\n", changed.Name, createdAccountsLabel(changed))
	}
	if opts.approval != "" {
		fmt.Fprintf(std.Stdout, "the access %s now uses the approval mode %s: %s\n", changed.Name, changed.Approval, approvalMeaning(changed.Approval))
	}
	return nil
}

func approvalMeaning(mode string) string {
	if mode == store.ApprovalApp {
		return "write tools run at once and the app asks for permission"
	}
	return "each write opens the approval window in the client"
}

func tokensCreate(ctx context.Context, args []string, std IO) error {
	opts, err := parseTokenOptions(args)
	if err != nil || opts.accounts == "" {
		return errUsage
	}
	created, err := ownerClient(std).CreateToken(ctx, opts.request())
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "created the access %s (static token, accounts: %s%s%s)\n", created.Access.Name, createdAccountsLabel(created.Access), readOnlySuffix(created.Access), approvalSuffix(created.Access))
	if len(created.Access.Accounts) == 0 {
		fmt.Fprintf(std.Stdout, "the access reaches no account yet; give it accounts with: zapgate access set %s --accounts <a,b|all>\n", created.Access.Name)
	}
	fmt.Fprintf(std.Stdout, "token: %s\n", created.Token)
	fmt.Fprintln(std.Stdout, "this is the only time the token is shown; store it now")
	return nil
}

func approvalSuffix(a admin.AccessView) string {
	if a.Approval == store.ApprovalApp {
		return ", approval: " + a.Approval
	}
	return ""
}

func readOnlySuffix(a admin.AccessView) string {
	if a.ReadOnly {
		return ", " + modeReadOnly
	}
	return ""
}

func accessList(ctx context.Context, args []string, std IO) error {
	if len(args) > 0 {
		return errUsage
	}
	list, err := ownerClient(std).Access(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(std.Stdout, noAccessMessage)
		return nil
	}
	rows := make([][]string, 0, len(list))
	for _, a := range list {
		rows = append(rows, []string{a.Name, a.Kind, accountsLabel(a), mode(a), a.Approval, a.CreatedAt})
	}
	return writeTable(std.Stdout, []string{"NAME", "KIND", "ACCOUNTS", "MODE", "APPROVAL", "CREATED"}, rows)
}

func accessRevoke(ctx context.Context, args []string, std IO) error {
	if len(args) != 1 {
		return errUsage
	}
	revoked, err := ownerClient(std).RevokeAccess(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "revoked the access %s; its next call gets 401\n", revoked.Name)
	return nil
}

func createdAccountsLabel(a admin.AccessView) string {
	if len(a.Accounts) == 0 {
		return noAccountsLabel
	}
	return strings.Join(a.Accounts, ",")
}

func accountsLabel(a admin.AccessView) string {
	if len(a.Accounts) == 0 {
		return noValue
	}
	return strings.Join(a.Accounts, ",")
}

func mode(a admin.AccessView) string {
	if a.ReadOnly {
		return modeReadOnly
	}
	return modeReadWrite
}

func orDash(value string) string {
	if value == "" {
		return noValue
	}
	return value
}

func writeTable(w io.Writer, header []string, rows [][]string) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if header != nil {
		fmt.Fprintln(table, strings.Join(header, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(table, strings.Join(row, "\t"))
	}
	return table.Flush()
}
