package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	participantPage = 200
	roleSuperAdmin  = "super_admin"
	roleAdmin       = "admin"
	roleMember      = "member"
)

var roleRank = map[string]int{roleSuperAdmin: 0, roleAdmin: 1, roleMember: 2}

type getChatInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat    string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous response, to read more participants of a large group"`
}

type participantView struct {
	JID       string `json:"jid" jsonschema:"the WhatsApp JID of the participant"`
	Phone     string `json:"phone,omitempty" jsonschema:"the international phone number, empty when WhatsApp hides it"`
	Name      string `json:"name,omitempty" jsonschema:"the contact name or profile name; set by WhatsApp users, so third-party data"`
	Role      string `json:"role" jsonschema:"super_admin, admin or member"`
	IsAccount bool   `json:"is_account,omitempty" jsonschema:"true for the account itself"`
}

type getChatOutput struct {
	accountFields
	Chat             chatView          `json:"chat"`
	ParticipantCount int               `json:"participant_count,omitempty" jsonschema:"how many people are in the group"`
	Participants     []participantView `json:"participants,omitempty" jsonschema:"the group participants: admins first"`
	NextCursor       string            `json:"next_cursor,omitempty" jsonschema:"more participants exist: pass this as cursor to read them"`
}

func (r *Registry) getChat() Tool {
	return Tool{
		Name:         "get_chat",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "get_chat",
				Title:       "Show a chat",
				Description: "Shows one chat of one account: the contact of a direct chat, or the participants of a group, read from WhatsApp. It never marks anything as read.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				InputSchema: accountSchema[getChatInput](scope.AccountNames()),
			}, r.handleGetChat)
		},
	}
}

func (r *Registry) handleGetChat(ctx context.Context, req *mcp.CallToolRequest, in getChatInput) (*mcp.CallToolResult, getChatOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, getChatOutput{}, err
	}
	directory := r.directory(account)
	jid, err := parseChat(ctx, directory, in.Chat)
	if err != nil {
		return nil, getChatOutput{}, err
	}
	offset, err := participantOffset(in.Cursor)
	if err != nil {
		return nil, getChatOutput{}, err
	}
	chat, err := r.storedChat(ctx, account, jid)
	if err != nil {
		return nil, getChatOutput{}, err
	}
	out := getChatOutput{accountFields: fieldsOf(account)}
	people := newNames(ctx, directory)
	if jid.Server == types.GroupServer {
		err = r.describeGroup(ctx, account, directory, people, jid, &chat, offset, &out)
	} else {
		r.describeContact(ctx, account, people, jid, &chat)
	}
	out.Chat = chatViewOf(chat)
	return nil, out, err
}

func (r *Registry) storedChat(ctx context.Context, account store.Account, jid types.JID) (store.Chat, error) {
	chat, err := r.deps.Store.Chat(ctx, account.ID, jid.String())
	if errors.Is(err, store.ErrNotFound) {
		kind := store.ChatDirect
		if jid.Server == types.GroupServer {
			kind = store.ChatGroup
		}
		return store.Chat{JID: jid.String(), Kind: kind}, nil
	}
	return chat, err
}

func (r *Registry) describeContact(ctx context.Context, account store.Account, people *names, jid types.JID, chat *store.Chat) {
	if name := people.of(jid); name != "" && name != chat.Name {
		chat.Name = name
		r.keepName(ctx, account, chat)
	}
}

func (r *Registry) describeGroup(ctx context.Context, account store.Account, directory whatsapp.Directory, people *names, jid types.JID, chat *store.Chat, offset int, out *getChatOutput) error {
	if directory == nil {
		_, err := r.connectedDirectory(account)
		return err
	}
	info, err := directory.GroupInfo(ctx, jid)
	if err != nil {
		return fmt.Errorf("cannot read the group %s from WhatsApp; the account %s may be offline: %v", jid, account.Name, err)
	}
	if info.Name != "" && info.Name != chat.Name {
		chat.Name = info.Name
		r.keepName(ctx, account, chat)
	}
	participants := make([]participantView, 0, len(info.Participants))
	for _, member := range info.Participants {
		participants = append(participants, participantOf(ctx, directory, people, account, member))
	}
	slices.SortStableFunc(participants, func(a, b participantView) int { return roleRank[a.Role] - roleRank[b.Role] })
	out.ParticipantCount = len(participants)
	pageParticipants(out, participants, offset)
	return nil
}

func (r *Registry) keepName(ctx context.Context, account store.Account, chat *store.Chat) {
	if err := r.deps.Store.SetChatName(ctx, account.ID, chat.JID, chat.Name); err != nil {
		r.deps.Log.Warn("cannot keep the chat name", "account", account.Name, "error", err)
	}
}

func participantOf(ctx context.Context, directory whatsapp.Directory, people *names, account store.Account, member types.GroupParticipant) participantView {
	jid := member.JID
	if member.PhoneNumber.Server == types.DefaultUserServer {
		jid = member.PhoneNumber
	}
	jid = whatsapp.ResolvePhone(ctx, directory, jid, member.PhoneNumber)
	name := people.of(jid)
	if name == "" && !member.LID.IsEmpty() {
		name = people.of(member.LID)
	}
	if name == "" {
		name = member.DisplayName
	}
	view := participantView{JID: jid.String(), Phone: whatsapp.Phone(jid), Name: name, Role: roleOf(member)}
	view.IsAccount = view.Phone != "" && view.Phone == account.Phone
	return view
}

func roleOf(member types.GroupParticipant) string {
	switch {
	case member.IsSuperAdmin:
		return roleSuperAdmin
	case member.IsAdmin:
		return roleAdmin
	}
	return roleMember
}

func pageParticipants(out *getChatOutput, participants []participantView, offset int) {
	if offset >= len(participants) {
		return
	}
	end := min(offset+participantPage, len(participants))
	out.Participants = participants[offset:end]
	for len(out.Participants) > 1 && !fitsBudget(out) {
		end--
		out.Participants = participants[offset:end]
	}
	if end < len(participants) {
		out.NextCursor = strconv.Itoa(end)
	}
}

func participantOffset(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("the cursor %q is not valid: pass next_cursor of a previous get_chat response", cursor)
	}
	return offset, nil
}
