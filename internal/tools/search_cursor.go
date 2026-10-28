package tools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
)

var errBadSearchCursor = errors.New("cursor is not a next_cursor of search_messages: pass next_cursor exactly as the previous response gave it")

type searchCursor struct {
	SentAt  int64  `json:"t"`
	ChatJID string `json:"c"`
	ID      string `json:"i"`
}

func encodeSearchCursor(msg store.Message) string {
	raw, _ := json.Marshal(searchCursor{SentAt: msg.SentAt.UnixMilli(), ChatJID: msg.ChatJID, ID: msg.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSearchCursor(text string) (*store.SearchPosition, error) {
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, errBadSearchCursor
	}
	var cursor searchCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ChatJID == "" || cursor.ID == "" {
		return nil, errBadSearchCursor
	}
	return &store.SearchPosition{SentAt: time.UnixMilli(cursor.SentAt), ChatJID: cursor.ChatJID, ID: cursor.ID}, nil
}
