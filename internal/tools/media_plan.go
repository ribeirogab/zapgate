package tools

import (
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	kilobyte = 1 << 10
	megabyte = 1 << 20
)

type mediaPlan struct {
	file      media.File
	kind      string
	caption   string
	voiceNote bool
}

func planFor(file media.File, in sendMediaInput) (mediaPlan, error) {
	plan := mediaPlan{file: file, kind: file.Kind(), caption: in.Caption, voiceNote: in.AsVoiceNote}
	if in.AsVoiceNote {
		if !media.Convertible(file.Mime) {
			return mediaPlan{}, fmt.Errorf("as_voice_note needs an audio or video file, and this file is %s; send it without as_voice_note", file.Mime)
		}
		plan.kind = media.KindAudio
	}
	if plan.kind == media.KindAudio && in.Caption != "" {
		return mediaPlan{}, fmt.Errorf("WhatsApp shows no caption on audio; send the caption with send_message")
	}
	return plan, nil
}

func (p mediaPlan) question(s sendTarget) string {
	kind := fmt.Sprintf("%s (%s)", p.kind, p.file.Mime)
	if p.voiceNote {
		kind = fmt.Sprintf("voice note, converted from %s", p.file.Mime)
	}
	question := s.question("Send this WhatsApp media?") + fmt.Sprintf("Kind: %s\nFile: %s\nSize: %s\nSHA-256: %s\n", kind, p.file.Name, size(p.file.Size), p.file.ShortHash())
	if p.caption != "" {
		question += "\nCaption:\n" + p.caption
	}
	return question
}

func size(bytes int64) string {
	switch {
	case bytes >= megabyte:
		return fmt.Sprintf("%.1f MB (%d bytes)", float64(bytes)/megabyte, bytes)
	case bytes >= kilobyte:
		return fmt.Sprintf("%.1f KB (%d bytes)", float64(bytes)/kilobyte, bytes)
	}
	return fmt.Sprintf("%d bytes", bytes)
}

func (p mediaPlan) message(ready media.Ready, up whatsmeow.UploadResponse, quote *waE2E.ContextInfo) *waE2E.Message {
	length := proto.Uint64(up.FileLength)
	mime := proto.String(ready.Mime)
	caption := optional(p.caption)
	switch p.kind {
	case media.KindImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: length,
			Mimetype: mime, Caption: caption, Width: optionalSize(ready.Width), Height: optionalSize(ready.Height), ContextInfo: quote,
		}}
	case media.KindVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: length,
			Mimetype: mime, Caption: caption, Seconds: optionalSize(ready.Seconds), ContextInfo: quote,
		}}
	case media.KindAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: length,
			Mimetype: mime, PTT: proto.Bool(p.voiceNote), Seconds: optionalSize(ready.Seconds), ContextInfo: quote,
		}}
	}
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: length,
		Mimetype: mime, Caption: caption, FileName: proto.String(ready.Name), Title: proto.String(ready.Name), ContextInfo: quote,
	}}
}

func (p mediaPlan) stored(ready media.Ready, up whatsmeow.UploadResponse) *store.Media {
	stored := &store.Media{
		Mime:          ready.Mime,
		Size:          int64(up.FileLength),
		Seconds:       ready.Seconds,
		VoiceNote:     p.voiceNote,
		MediaKey:      up.MediaKey,
		DirectPath:    up.DirectPath,
		FileSHA256:    up.FileSHA256,
		FileEncSHA256: up.FileEncSHA256,
	}
	if p.kind == media.KindDocument {
		stored.FileName = ready.Name
	}
	return stored
}

func optional(text string) *string {
	if text == "" {
		return nil
	}
	return proto.String(text)
}

func optionalSize(value int) *uint32 {
	if value <= 0 {
		return nil
	}
	return proto.Uint32(uint32(value))
}
