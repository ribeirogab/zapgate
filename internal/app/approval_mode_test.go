package app_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/admin"
	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestAnAccessThatTrustsTheAppWritesEvenWhenTheClientDeclinesEveryApproval(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := approvalSession(t, h, path.protocol, "decline")

			set := h.CLI(t, "access", "set", "bot", "--approval", "app")

			if set.Code != 0 || set.Stdout != "the access bot now uses the approval mode app: write tools run at once and the app asks for permission\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
			}
			result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}))

			if result.IsError {
				t.Fatalf("send_message failed: %s", errorText(result))
			}
			if got := structured(t, result); !strings.Contains(got, `"approval":"access_trusts_app"`) {
				t.Fatalf("result %s", got)
			}
			if asked := o.questions(); len(asked) != 0 {
				t.Fatalf("the owner was asked %q", asked)
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].Message.GetConversation() != "Chego às 15h" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
		})
	}
}

func TestSwitchingBackToElicitationOpensTheApprovalWindowOnTheNextCall(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := approvalSession(t, h, path.protocol, "decline")
			h.CLI(t, "access", "set", "bot", "--approval", "app")
			if trusted := callTool(t, session, "react", withWork(map[string]any{"chat": mariaJID.String(), "message_id": "3EB0M1", "emoji": "👍"})); trusted.IsError {
				t.Fatalf("react with the app mode failed: %s", errorText(trusted))
			}

			set := h.CLI(t, "access", "set", "bot", "--approval", "elicitation")

			if set.Code != 0 || set.Stdout != "the access bot now uses the approval mode elicitation: each write opens the approval window in the client\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
			}
			result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}))

			if want := "the owner did not approve (decline): the message was not sent"; !result.IsError || errorText(result) != want {
				t.Fatalf("error %v %q, want %q", result.IsError, errorText(result), want)
			}
			if asked := o.questions(); len(asked) != 1 || !strings.HasPrefix(asked[0], "Send this WhatsApp message?") {
				t.Fatalf("the owner was asked %q", asked)
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].Message.GetReactionMessage().GetText() != "👍" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
		})
	}
}

func TestAccessSetChangesTheAccountsAndTheApprovalModeTogether(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	h.StaticToken(t, "bot", []string{"work"}, false)

	set := h.CLI(t, "access", "set", "bot", "--accounts", "personal", "--approval", "app")

	want := "the access bot now reaches the accounts: personal\nthe access bot now uses the approval mode app: write tools run at once and the app asks for permission\n"
	if set.Code != 0 || set.Stdout != want {
		t.Fatalf("exit %d, stdout %q, stderr %q", set.Code, set.Stdout, set.Stderr)
	}
	if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); rows[1] != "bot static personal read-write app 2026-10-13T12:00:00Z" {
		t.Fatalf("access list %q", rows)
	}
}

func TestATokenCreatedWithTheAppModeWritesWithoutTheApprovalWindow(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))

	created := h.CLI(t, "tokens", "create", "cursor", "--accounts", "work", "--approval", "app")
	h.CLI(t, "tokens", "create", "bot", "--accounts", "work")

	if !strings.HasPrefix(created.Stdout, "created the access cursor (static token, accounts: work, approval: app)\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", created.Code, created.Stdout, created.Stderr)
	}
	o := &owner{answer: "decline"}
	session := h.Connect(t, tokenFrom(t, created.Stdout), o.elicitation())
	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))
	if result.IsError || !strings.Contains(structured(t, result), `"approval":"access_trusts_app"`) || len(o.questions()) != 0 {
		t.Fatalf("error %v %q, result %s, asked %q", result.IsError, errorText(result), structured(t, result), o.questions())
	}
	rows := tableRows(t, h.CLI(t, "access", "list").Stdout)
	want := []string{
		"NAME KIND ACCOUNTS MODE APPROVAL CREATED",
		"bot static work read-write elicitation 2026-10-13T12:00:00Z",
		"cursor static work read-write app 2026-10-13T12:00:00Z",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("access list\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func TestAReadOnlyAccessWithTheAppModeStillSeesNoWriteTool(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	created := h.CLI(t, "tokens", "create", "reader", "--accounts", "work", "--read-only", "--approval", "app")

	session := h.Connect(t, tokenFrom(t, created.Stdout), (&owner{answer: "decline"}).elicitation())

	if names := strings.Join(toolNames(listTools(t, session)), ","); strings.Contains(names, "send_message") || strings.Contains(names, "react") || strings.Contains(names, "mark_read") {
		t.Fatalf("a read-only access sees %s", names)
	}
}

func TestApprovalModeChangesAreAudited(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.CLI(t, "tokens", "create", "cursor", "--accounts", "work", "--approval", "app")
	h.CLI(t, "access", "set", "cursor", "--approval", "elicitation")
	h.CLI(t, "access", "set", "cursor", "--accounts", "all")

	actions := linesWithMessage(h.AuditLines(t), "admin")
	want := []map[string]string{
		{"action": "account_add", "account": "work"},
		{"action": "token_create", "grant": "cursor", "approval": "app"},
		{"action": "access_set", "grant": "cursor", "approval": "elicitation"},
		{"action": "access_set", "grant": "cursor", "approval": "elicitation"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audit has %d admin lines, want %d: %s", len(actions), len(want), h.AuditRaw())
	}
	for i, fields := range want {
		assertFields(t, actions[i], fields)
	}
}

func TestOwnerAPIRejectsAnUnclearAccessChange(t *testing.T) {
	cases := map[string]admin.SetAccessRequest{
		"nothing to change":  {},
		"unknown mode":       {Approval: "auto"},
		"empty account list": {Accounts: []string{}, Approval: "app"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			h := apptest.Start(t)
			h.StaticToken(t, "bot", nil, false)

			_, err := h.Owner.SetAccess(context.Background(), "bot", req)

			var apiErr *admin.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
				t.Fatalf("error %v, want a 400 from the owner API", err)
			}
			if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); rows[1] != "bot static - read-write elicitation 2026-10-13T12:00:00Z" {
				t.Fatalf("a refused change changed the access: %q", rows)
			}
		})
	}
}

func TestOwnerAPIRejectsAnUnknownApprovalModeForANewToken(t *testing.T) {
	h := apptest.Start(t)

	_, err := h.Owner.CreateToken(context.Background(), admin.CreateTokenRequest{Name: "bot", AllExistingAccounts: true, Approval: "auto"})

	var apiErr *admin.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Message != `the approval mode "auto" is not valid: use elicitation or app` {
		t.Fatalf("error %v, want a 400 from the owner API", err)
	}
}
