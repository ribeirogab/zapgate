package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

const (
	MessageImage    = "image"
	MessageVideo    = "video"
	MessageAudio    = "audio"
	MessageDocument = "document"
	MessageSticker  = "sticker"
	MessageLocation = "location"
	MessageContact  = "contact"
	MessagePoll     = "poll"
)

type Media struct {
	Mime          string
	FileName      string
	Size          int64
	Seconds       int
	VoiceNote     bool
	MediaKey      []byte
	DirectPath    string
	FileSHA256    []byte
	FileEncSHA256 []byte
	State         string
	Path          string
}

type Details struct {
	Poll     *Poll         `json:"poll,omitempty"`
	Location *Location     `json:"location,omitempty"`
	Contacts []ContactCard `json:"contacts,omitempty"`
}

type Poll struct {
	Options    []string `json:"options"`
	MaxChoices int      `json:"max_choices"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	URL       string  `json:"url,omitempty"`
	Live      bool    `json:"live,omitempty"`
}

type ContactCard struct {
	Name  string `json:"name"`
	VCard string `json:"vcard"`
}

func insertParts(ctx context.Context, tx *sql.Tx, accountID int64, msg Message) error {
	if msg.Media != nil {
		if err := insertMedia(ctx, tx, accountID, msg); err != nil {
			return err
		}
	}
	if msg.Details != nil {
		return insertDetails(ctx, tx, accountID, msg)
	}
	return nil
}

func insertMedia(ctx context.Context, tx *sql.Tx, accountID int64, msg Message) error {
	m := msg.Media
	state := MediaPending
	if m.Path != "" {
		state = MediaDownloaded
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO media (account_id, chat_jid, message_id, mime, file_name, size, seconds, voice_note, media_key, direct_path, file_sha256, file_enc_sha256, state, path)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		accountID, msg.ChatJID, msg.ID, optionalText(m.Mime), optionalText(m.FileName), optionalInt(m.Size), optionalInt(int64(m.Seconds)), m.VoiceNote,
		m.MediaKey, optionalText(m.DirectPath), m.FileSHA256, m.FileEncSHA256, state, optionalText(m.Path))
	return err
}

func insertDetails(ctx context.Context, tx *sql.Tx, accountID int64, msg Message) error {
	body, err := json.Marshal(msg.Details)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO message_details (account_id, chat_jid, message_id, details) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		accountID, msg.ChatJID, msg.ID, string(body))
	return err
}

type partColumns struct {
	mime, fileName, state, details sql.NullString
	size, seconds                  sql.NullInt64
	voiceNote                      sql.NullBool
}

func (p partColumns) attachTo(msg *Message) error {
	if p.state.Valid {
		msg.Media = &Media{
			Mime:      p.mime.String,
			FileName:  p.fileName.String,
			Size:      p.size.Int64,
			Seconds:   int(p.seconds.Int64),
			VoiceNote: p.voiceNote.Bool,
			State:     p.state.String,
		}
	}
	if !p.details.Valid {
		return nil
	}
	msg.Details = &Details{}
	return json.Unmarshal([]byte(p.details.String), msg.Details)
}

func optionalInt(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: value != 0}
}
