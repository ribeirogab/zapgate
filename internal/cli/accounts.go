package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mdp/qrterminal/v3"

	"github.com/ribeirogab/zapgate/internal/admin"
)

const (
	phoneFlag       = "--phone"
	descriptionFlag = "--description"
	qrFlag          = "--qr"
	yesFlag         = "--yes"
)

var errRemovalNotConfirmed = errors.New("the account stays; answer y to remove it, or pass --yes")

func parseAddOptions(args []string) (admin.AddAccountRequest, error) {
	var req admin.AddAccountRequest
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == qrFlag:
			req.QR = true
		case (arg == phoneFlag || arg == descriptionFlag) && i+1 < len(args):
			i++
			setAddValue(&req, arg, args[i])
		case strings.HasPrefix(arg, phoneFlag+"="):
			setAddValue(&req, phoneFlag, strings.TrimPrefix(arg, phoneFlag+"="))
		case strings.HasPrefix(arg, descriptionFlag+"="):
			setAddValue(&req, descriptionFlag, strings.TrimPrefix(arg, descriptionFlag+"="))
		case !strings.HasPrefix(arg, "-") && req.Name == "":
			req.Name = arg
		default:
			return req, errUsage
		}
	}
	if req.Name == "" || req.Phone == "" {
		return req, errUsage
	}
	return req, nil
}

func setAddValue(req *admin.AddAccountRequest, flag, value string) {
	if flag == phoneFlag {
		req.Phone = value
		return
	}
	req.Description = value
}

func accountsAdd(ctx context.Context, args []string, std IO) error {
	req, err := parseAddOptions(args)
	if err != nil {
		return err
	}
	screen := &pairingScreen{out: std.Stdout, name: req.Name}
	account, err := ownerClient(std).AddAccount(ctx, req, screen.show)
	if err != nil {
		return err
	}
	fmt.Fprintf(std.Stdout, "the account %s is connected (%s%s)\n", account.Name, account.Phone, profileSuffix(account))
	return nil
}

type pairingScreen struct {
	out      io.Writer
	name     string
	codeSeen bool
	qrSeen   bool
}

func (s *pairingScreen) show(event admin.PairingEvent) {
	switch event.Event {
	case admin.PairingCode:
		s.showCode(event.Code)
	case admin.PairingQR:
		s.showQR(event.QR)
	}
}

func (s *pairingScreen) showCode(code string) {
	if s.codeSeen {
		return
	}
	s.codeSeen = true
	fmt.Fprintf(s.out, "on the phone of the account %s, open WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, and type this code:\n\n    %s\n\nwaiting for the phone...\n", s.name, code)
}

func (s *pairingScreen) showQR(qr string) {
	if s.qrSeen {
		fmt.Fprintln(s.out, "the QR code changed; scan this one:")
	} else {
		fmt.Fprintf(s.out, "on the phone of the account %s, open WhatsApp > Settings > Linked devices > Link a device, and scan this QR code:\n", s.name)
	}
	s.qrSeen = true
	qrterminal.GenerateHalfBlock(qr, qrterminal.L, s.out)
	fmt.Fprintln(s.out, "waiting for the phone...")
}

func profileSuffix(a admin.AccountView) string {
	if a.ProfileName == "" {
		return ""
	}
	return ", profile " + a.ProfileName
}

func accountsDescribe(ctx context.Context, args []string, std IO) error {
	if len(args) < 2 {
		return errUsage
	}
	description := strings.Join(args[1:], " ")
	account, err := ownerClient(std).DescribeAccount(ctx, args[0], description)
	if err != nil {
		return err
	}
	if account.Description == "" {
		fmt.Fprintf(std.Stdout, "the account %s has no description now\n", account.Name)
		return nil
	}
	fmt.Fprintf(std.Stdout, "the account %s has the description: %s\n", account.Name, account.Description)
	return nil
}

func accountsRemove(ctx context.Context, args []string, std IO) error {
	name, confirmed, err := parseRemoveOptions(args)
	if err != nil {
		return err
	}
	owner := ownerClient(std)
	account, err := owner.Account(ctx, name)
	if err != nil {
		return err
	}
	if !confirmed && !confirmRemoval(std, account) {
		return errRemovalNotConfirmed
	}
	removed, err := owner.RemoveAccount(ctx, name)
	if err != nil {
		return err
	}
	if removed.Warning != "" {
		fmt.Fprintf(std.Stderr, "warning: %s\n", removed.Warning)
	}
	fmt.Fprintf(std.Stdout, "removed the account %s\n", removed.Account.Name)
	return nil
}

func parseRemoveOptions(args []string) (string, bool, error) {
	var (
		name      string
		confirmed bool
	)
	for _, arg := range args {
		switch {
		case arg == yesFlag:
			confirmed = true
		case !strings.HasPrefix(arg, "-") && name == "":
			name = arg
		default:
			return "", false, errUsage
		}
	}
	if name == "" {
		return "", false, errUsage
	}
	return name, confirmed, nil
}

func confirmRemoval(std IO, account admin.AccountView) bool {
	fmt.Fprintf(std.Stdout, "remove the account %s (%s)? This logs out its device on WhatsApp, deletes its local data, and takes it out of every access. [y/N] ", account.Name, account.Phone)
	answer, _ := bufio.NewReader(std.Stdin).ReadString('\n')
	fmt.Fprintln(std.Stdout)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}
