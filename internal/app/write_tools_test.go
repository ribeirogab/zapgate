package app_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var writeTools = map[string]map[string]any{
	"send_message":   {"to": mariaJID.String(), "text": "oi"},
	"send_media":     {"to": mariaJID.String(), "base64": "aGVsbG8="},
	"react":          {"chat": mariaJID.String(), "message_id": "3EB0M1", "emoji": "👍"},
	"edit_message":   {"chat": mariaJID.String(), "message_id": "3EB0M1", "text": "oi"},
	"delete_message": {"chat": mariaJID.String(), "message_id": "3EB0M1"},
	"mark_read":      {"chat": mariaJID.String(), "message_ids": []string{"3EB0M1"}},
}

func TestWriteToolsAreDestructiveAndMarkReadIsNeither(t *testing.T) {
	h := apptest.Start(t)
	session := writerSession(t, h)

	tools := listTools(t, session)

	for name := range writeTools {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("the tool %s is missing", name)
		}
		destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
		if tool.Annotations.ReadOnlyHint || destructive != (name != "mark_read") {
			t.Errorf("%s has readOnlyHint %v and destructiveHint %v", name, tool.Annotations.ReadOnlyHint, destructive)
		}
		schema := schemaOf(t, tool)
		if !slices.Contains(schema.Required, "account") || !slices.Equal(schema.Properties["account"].Enum, []string{"work"}) {
			t.Errorf("%s does not require an account of the access: %+v", name, schema)
		}
	}
}

func TestAReadOnlyAccessNeitherSeesNorCallsWriteTools(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("oi")))
			created := h.CLI(t, "tokens", "create", "reader", "--accounts", "work", "--read-only")
			if created.Code != 0 {
				t.Fatalf("tokens create: %q", created.Stderr)
			}
			token := tokenFrom(t, created.Stdout)
			session := h.Connect(t, token, apptest.WithProtocol(path.protocol), (&owner{answer: "accept"}).elicitation())

			tools := listTools(t, session)

			for name, args := range writeTools {
				if _, ok := tools[name]; ok {
					t.Errorf("a read-only access sees %s", name)
				}
				_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: withWork(args)})
				if err == nil || !strings.Contains(err.Error(), "unknown tool") {
					t.Errorf("a read-only access called %s: %v", name, err)
				}
			}
			if len(h.WhatsApp.Sent(workPhone)) != 0 || len(h.WhatsApp.Receipts(workPhone)) != 0 {
				t.Fatal("a read-only access changed WhatsApp")
			}
		})
	}
}

func TestWriteToolsRefuseAMissingOrForeignAccount(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false))

	for name, args := range writeTools {
		for _, account := range []any{nil, "", "personal"} {
			call := map[string]any{}
			for key, value := range args {
				call[key] = value
			}
			if account != nil {
				call["account"] = account
			}

			result := callTool(t, session, name, call)

			if !result.IsError || !strings.Contains(errorText(result), "account") {
				t.Errorf("%s with account %v: error %v %q", name, account, result.IsError, errorText(result))
			}
		}
	}
	if len(h.WhatsApp.Sent(personalPhone)) != 0 || len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatal("a refused call reached WhatsApp")
	}
}
