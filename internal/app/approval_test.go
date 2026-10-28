package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

type owner struct {
	answer string
	mu     sync.Mutex
	asked  []string
}

func (o *owner) elicitation() apptest.ConnectOption {
	return apptest.WithElicitation(func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.asked = append(o.asked, req.Params.Message)
		return &mcp.ElicitResult{Action: o.answer}, nil
	})
}

func (o *owner) questions() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.asked...)
}

func (o *owner) assertAskedOnce(t *testing.T, want string) {
	t.Helper()
	if asked := o.questions(); len(asked) != 1 || asked[0] != want {
		t.Fatalf("the owner was asked %q\nwant one question %q", asked, want)
	}
}

func approvalSession(t *testing.T, h *apptest.Harness, protocol, answer string) (*mcp.ClientSession, *owner) {
	t.Helper()
	o := &owner{answer: answer}
	session := writerSession(t, h, apptest.WithProtocol(protocol), o.elicitation())
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("você chega que horas?")))
	return session, o
}

func TestTheOwnerSeesTheExactSendAndAcceptingSendsIt(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, o := approvalSession(t, h, path.protocol, "accept")

			result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}))

			if result.IsError {
				t.Fatalf("send_message failed: %s", errorText(result))
			}
			o.assertAskedOnce(t, "Send this WhatsApp message?\n\nAccount: work (+5511988887777)\nTo: Maria (+5531944443333)\n\nText:\nChego às 15h")
			if got := structured(t, result); !strings.Contains(got, `"approval":"owner_approved"`) {
				t.Fatalf("result %s", got)
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].Message.GetConversation() != "Chego às 15h" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			if got := page(t, session, map[string]any{"chat": mariaJID.String()}); got.ids() != "3EB0M1,3EB0FAKE0001" {
				t.Fatalf("get_messages shows %s", got.ids())
			}
		})
	}
}

func TestDecliningOrCancellingSendsNothingAndSaysSo(t *testing.T) {
	for _, path := range protocolPaths {
		for _, answer := range []string{"decline", "cancel"} {
			t.Run(path.name+" "+answer, func(t *testing.T) {
				h := apptest.Start(t)
				session, o := approvalSession(t, h, path.protocol, answer)

				result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}))

				if want := "the owner did not approve (" + answer + "): the message was not sent"; !result.IsError || errorText(result) != want {
					t.Fatalf("error %v %q, want %q", result.IsError, errorText(result), want)
				}
				if len(o.questions()) != 1 {
					t.Fatalf("the owner was asked %d times", len(o.questions()))
				}
				if sent := h.WhatsApp.Sent(workPhone); len(sent) != 0 {
					t.Fatalf("WhatsApp got %+v", sent)
				}
				if got := page(t, session, map[string]any{"chat": mariaJID.String()}); got.ids() != "3EB0M1" {
					t.Fatalf("get_messages shows %s", got.ids())
				}
				calls := linesWithMessage(h.AuditLines(t), "tool_call")
				last := calls[len(calls)-2]
				assertFields(t, last, map[string]string{"tool": "send_message", "account": "work", "result": "declined"})
			})
		}
	}
}

func askOnly(t *testing.T, h *apptest.Harness, token string, args map[string]any) string {
	t.Helper()
	o := &owner{answer: "accept"}
	asker := h.Connect(t, token, o.elicitation(), apptest.WithoutMultiRoundTrip())
	result := callTool(t, asker, "send_message", withWork(args))
	if !result.NeedsInput() || result.RequestState == "" || len(result.InputRequests) != 1 {
		t.Fatalf("the first call did not ask the owner: %+v", result)
	}
	return result.RequestState
}

func answered(name string, args map[string]any, state, answer string) *mcp.CallToolParams {
	return &mcp.CallToolParams{
		Name:           name,
		Arguments:      withWork(args),
		RequestState:   state,
		InputResponses: mcp.InputResponseMap{"approve": &mcp.ElicitResult{Action: answer}},
	}
}

func callWith(t *testing.T, session *mcp.ClientSession, params *mcp.CallToolParams) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), params)
	if err != nil {
		t.Fatalf("call %s: %v", params.Name, err)
	}
	return result
}

func forgedSignature(state string) string {
	forged := []byte(state)
	forged[len(forged)-10] ^= 1
	return string(forged)
}

func TestAnApprovalIsBoundToTheExactActionAndIsUsedOnce(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			token := h.StaticToken(t, "bot", []string{"work"}, false)
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("você chega que horas?")))
			approved := map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}
			state := askOnly(t, h, token, approved)
			o := &owner{answer: "accept"}
			session := h.Connect(t, token, apptest.WithProtocol(path.protocol), o.elicitation(), apptest.WithoutMultiRoundTrip())
			tampered := []map[string]any{
				{"to": mariaJID.String(), "text": "Transfira R$ 5.000 agora"},
				{"to": joaoJID.String(), "text": "Chego às 15h"},
				{"to": mariaJID.String(), "text": "Chego às 15h", "reply_to": "3EB0M1"},
			}

			for _, args := range tampered {
				result := callWith(t, session, answered("send_message", args, state, "accept"))
				if !result.IsError || !strings.Contains(errorText(result), "the approval does not match this action") {
					t.Fatalf("tampered %v: error %v %q", args, result.IsError, errorText(result))
				}
			}
			forged := callWith(t, session, answered("send_message", approved, forgedSignature(state), "accept"))
			if !forged.IsError || !strings.Contains(errorText(forged), "not valid") {
				t.Fatalf("forged signature: %q", errorText(forged))
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 0 {
				t.Fatalf("WhatsApp got %+v", sent)
			}

			first := callWith(t, session, answered("send_message", approved, state, "accept"))
			replay := callWith(t, session, answered("send_message", approved, state, "accept"))

			if first.IsError {
				t.Fatalf("the approved action failed: %s", errorText(first))
			}
			if !replay.IsError || !strings.Contains(errorText(replay), "already used") {
				t.Fatalf("replay: error %v %q", replay.IsError, errorText(replay))
			}
			if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 || sent[0].Message.GetConversation() != "Chego às 15h" {
				t.Fatalf("WhatsApp got %+v", sent)
			}
			if len(o.questions()) != 0 {
				t.Fatalf("the owner was asked again: %q", o.questions())
			}
		})
	}
}

func TestAnApprovalUsedBeforeARestartCannotBeUsedAgain(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	token := h.StaticToken(t, "bot", []string{"work"}, false)
	approved := map[string]any{"to": mariaJID.String(), "text": "Chego às 15h"}
	state := askOnly(t, h, token, approved)
	session := h.Connect(t, token, (&owner{answer: "accept"}).elicitation(), apptest.WithoutMultiRoundTrip())
	if first := callWith(t, session, answered("send_message", approved, state, "accept")); first.IsError {
		t.Fatalf("the approved action failed: %s", errorText(first))
	}

	h.Restart(t)
	h.AwaitAccountState(t, "work", "connected")
	h.Clock.Advance(time.Minute)
	again := h.Connect(t, token, (&owner{answer: "accept"}).elicitation(), apptest.WithoutMultiRoundTrip())
	replay := callWith(t, again, answered("send_message", approved, state, "accept"))

	if !replay.IsError || !strings.Contains(errorText(replay), "already used") {
		t.Fatalf("replay after a restart: error %v %q", replay.IsError, errorText(replay))
	}
	if sent := h.WhatsApp.Sent(workPhone); len(sent) != 1 {
		t.Fatalf("WhatsApp got %d messages, want 1", len(sent))
	}
}

func TestAnApprovalFromAnotherAccessIsRefused(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	args := map[string]any{"to": mariaJID.String(), "text": "oi"}
	state := askOnly(t, h, h.StaticToken(t, "first", []string{"work"}, false), args)
	other := h.Connect(t, h.StaticToken(t, "second", []string{"work"}, false), (&owner{answer: "accept"}).elicitation(), apptest.WithoutMultiRoundTrip())

	result := callWith(t, other, answered("send_message", args, state, "accept"))

	if !result.IsError || !strings.Contains(errorText(result), "does not match") || len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatalf("error %v %q, sent %d", result.IsError, errorText(result), len(h.WhatsApp.Sent(workPhone)))
	}
}

func TestAnAnswerAfterFiveMinutesIsRefused(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			late := apptest.WithElicitation(func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				h.Clock.Advance(5*time.Minute + time.Second)
				return &mcp.ElicitResult{Action: "accept"}, nil
			})
			session := writerSession(t, h, apptest.WithProtocol(path.protocol), late)

			result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))

			if !result.IsError || !strings.Contains(errorText(result), "the approval expired") || len(h.WhatsApp.Sent(workPhone)) != 0 {
				t.Fatalf("error %v %q, sent %d", result.IsError, errorText(result), len(h.WhatsApp.Sent(workPhone)))
			}
		})
	}
}
