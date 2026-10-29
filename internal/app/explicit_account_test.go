package app_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

var accountTools = map[string]map[string]any{
	"get_messages":    {"chat": mariaJID.String()},
	"list_chats":      {},
	"get_chat":        {"chat": mariaJID.String()},
	"search_contacts": {"query": "maria"},
}

type toolSchema struct {
	Required   []string `json:"required"`
	Properties map[string]struct {
		Enum []string `json:"enum"`
	} `json:"properties"`
}

func schemaOf(t *testing.T, tool *mcp.Tool) toolSchema {
	t.Helper()
	body, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema toolSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func listTools(t *testing.T, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func TestEveryToolButListAccountsRequiresAnAccountOfTheAccess(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false))

	tools := listTools(t, session)

	for name := range accountTools {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("the tool %s is missing", name)
		}
		schema := schemaOf(t, tool)
		if !slices.Contains(schema.Required, "account") {
			t.Errorf("%s does not require account: %v", name, schema.Required)
		}
		if got := schema.Properties["account"].Enum; !slices.Equal(got, []string{"work"}) {
			t.Errorf("%s offers the accounts %v, want only work", name, got)
		}
	}
	if _, ok := tools["list_accounts"]; !ok || len(schemaOf(t, tools["list_accounts"]).Required) != 0 {
		t.Fatal("list_accounts must exist and take no account")
	}
}

func TestAToolRefusesAMissingOrForeignAccount(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.AddAccount(t, "personal", personalPhone, "Alex")
			session := h.Connect(t, h.StaticToken(t, "bot", []string{"work"}, false), apptest.WithProtocol(path.protocol))
			cases := map[string]map[string]any{"missing": nil, "empty": {"account": ""}, "foreign": {"account": "personal"}, "unknown": {"account": "sales"}}

			for name, args := range accountTools {
				for label, account := range cases {
					call := map[string]any{}
					for key, value := range args {
						call[key] = value
					}
					for key, value := range account {
						call[key] = value
					}

					result := callTool(t, session, name, call)

					if !result.IsError || !strings.Contains(errorText(result), "account") {
						t.Errorf("%s with a %s account: error %v, text %q", name, label, result.IsError, errorText(result))
					}
				}
			}
		})
	}
}

func TestAnAccessWithoutAccountsSeesOnlyListAccountsUntilItGetsOne(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session := h.Connect(t, h.StaticToken(t, "bot", nil, false), apptest.WithProtocol(path.protocol))
			h.AddAccount(t, "work", workPhone, "Alex")

			if names := toolNames(listTools(t, session)); !slices.Equal(names, []string{"list_accounts"}) {
				t.Fatalf("an access without accounts sees %v", names)
			}

			if set := h.CLI(t, "access", "set", "bot", "--accounts", "work"); set.Code != 0 {
				t.Fatalf("access set: %q", set.Stderr)
			}

			tools := listTools(t, session)
			for name := range accountTools {
				if tool, ok := tools[name]; !ok || !slices.Equal(schemaOf(t, tool).Properties["account"].Enum, []string{"work"}) {
					t.Fatalf("after access set, %s is %+v", name, tool)
				}
			}
		})
	}
}

func toolNames(tools map[string]*mcp.Tool) []string {
	var names []string
	for name := range tools {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
