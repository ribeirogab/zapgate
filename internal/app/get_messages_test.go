package app_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

type messagesPage struct {
	Messages []struct {
		ID         string `json:"id"`
		ThirdParty struct {
			Text          string `json:"text"`
			TextTruncated bool   `json:"text_truncated"`
		} `json:"third_party"`
	} `json:"messages"`
	OlderBefore string `json:"older_before"`
	NewerAfter  string `json:"newer_after"`
}

func (p messagesPage) ids() string {
	var ids []string
	for _, msg := range p.Messages {
		ids = append(ids, msg.ID)
	}
	return strings.Join(ids, ",")
}

func page(t *testing.T, session *mcp.ClientSession, args map[string]any) messagesPage {
	t.Helper()
	var p messagesPage
	decode(t, getMessages(t, session, withWork(args)).StructuredContent, &p)
	return p
}

func sevenMessages(t *testing.T, h *apptest.Harness) *mcp.ClientSession {
	t.Helper()
	session := workSession(t, h)
	for i, at := range []int{0, 1, 2, 2, 3, 4, 5} {
		id := fmt.Sprintf("M%d", i+1)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, id, "Maria", morning.Add(time.Duration(at)*time.Minute)), text("mensagem "+id)))
	}
	return session
}

func TestGetMessagesPagesBackAndForth(t *testing.T) {
	h := apptest.Start(t)
	session := sevenMessages(t, h)
	chat := mariaJID.String()
	cases := []struct {
		name  string
		args  map[string]any
		ids   string
		older string
		newer string
	}{
		{name: "newest", args: map[string]any{"limit": 3}, ids: "M5,M6,M7", older: "M5"},
		{name: "before an id", args: map[string]any{"limit": 3, "before": "M5"}, ids: "M2,M3,M4", older: "M2", newer: "M4"},
		{name: "before the oldest", args: map[string]any{"before": "M2"}, ids: "M1", newer: "M1"},
		{name: "after an id", args: map[string]any{"limit": 2, "after": "M2"}, ids: "M3,M4", older: "M3", newer: "M4"},
		{name: "after a tie", args: map[string]any{"limit": 2, "after": "M3"}, ids: "M4,M5", older: "M4", newer: "M5"},
		{name: "after a time", args: map[string]any{"after": "2026-10-13T09:03:30Z"}, ids: "M6,M7", older: "M6"},
		{name: "before a time", args: map[string]any{"before": "2026-10-13T09:02:00Z"}, ids: "M1,M2", newer: "M2"},
		{name: "between", args: map[string]any{"after": "M1", "before": "M6"}, ids: "M2,M3,M4,M5", older: "M2", newer: "M5"},
		{name: "all", args: map[string]any{}, ids: "M1,M2,M3,M4,M5,M6,M7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"chat": chat}
			for key, value := range tc.args {
				args[key] = value
			}

			got := page(t, session, args)

			if got.ids() != tc.ids || got.OlderBefore != tc.older || got.NewerAfter != tc.newer {
				t.Fatalf("ids %s older %q newer %q, want %s %q %q", got.ids(), got.OlderBefore, got.NewerAfter, tc.ids, tc.older, tc.newer)
			}
		})
	}
}

func TestGetMessagesRefusesBadArguments(t *testing.T) {
	h := apptest.Start(t)
	session := sevenMessages(t, h)
	cases := map[string]map[string]any{
		"unknown before":  {"chat": mariaJID.String(), "before": "NOPE"},
		"unknown chat":    {"chat": joaoJID.String()},
		"chat not valid":  {"chat": "maria"},
		"limit too large": {"chat": mariaJID.String(), "limit": 201},
		"limit zero":      {"chat": mariaJID.String(), "limit": 0.5},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			result := callTool(t, session, "get_messages", withWork(args))

			if !result.IsError {
				t.Fatalf("accepted: %s", structured(t, result))
			}
		})
	}
}

func TestALongHistoryComesInPagesUnderTheSizeCap(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	long := strings.Repeat("palavra ", 150)
	for i := range 300 {
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, fmt.Sprintf("L%03d", i), "Maria", morning.Add(time.Duration(i)*time.Second)), text(fmt.Sprintf("%d %s", i, long))))
	}
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "HUGE", "Maria", morning.Add(time.Hour)), text(strings.Repeat("x", 60_000))))

	var seen []string
	args := map[string]any{"chat": mariaJID.String(), "limit": 200}
	for pages := 0; ; pages++ {
		result := getMessages(t, session, withWork(args))
		if size := encodedSize(t, result); size > 40_000 {
			t.Fatalf("a page with its text copy has %d bytes, more than about 10 thousand tokens", size)
		}
		var p messagesPage
		decode(t, result.StructuredContent, &p)
		seen = append(strings.Split(p.ids(), ","), seen...)
		if p.OlderBefore == "" {
			break
		}
		if pages > 100 {
			t.Fatal("the pages do not end")
		}
		args["before"] = p.OlderBefore
	}
	if len(seen) != 301 || seen[0] != "L000" || seen[300] != "HUGE" || len(slices.Compact(slices.Clone(seen))) != 301 {
		t.Fatalf("read %d messages, first %s, last %s", len(seen), seen[0], seen[len(seen)-1])
	}
	huge := page(t, session, map[string]any{"chat": mariaJID.String(), "limit": 1})
	if !huge.Messages[0].ThirdParty.TextTruncated || len(huge.Messages[0].ThirdParty.Text) >= 60_000 {
		t.Fatal("a very long text was not cut")
	}
}

func encodedSize(t *testing.T, result *mcp.CallToolResult) int {
	t.Helper()
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return len(body)
}
