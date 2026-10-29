package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow"

	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/store"
)

const sendMediaName = "send_media"

type sendMediaInput struct {
	Account     string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	To          string `json:"to" jsonschema:"the phone number with the country code, for example +5511988887777, or a chat JID from list_chats"`
	MediaURL    string `json:"media_url,omitempty" jsonschema:"a public http or https URL of the file, up to 100 MB; pass exactly one of media_url, upload_id or base64"`
	UploadID    string `json:"upload_id,omitempty" jsonschema:"the upload_id from POST /uploads with the same token, valid for 1 hour; the best way for an agent with a shell"`
	Base64      string `json:"base64,omitempty" jsonschema:"the file in base64 or as a data URL, up to 5 MB after decoding"`
	Caption     string `json:"caption,omitempty" jsonschema:"the text under an image, video or document; audio has no caption"`
	FileName    string `json:"file_name,omitempty" jsonschema:"the file name the person sees on a document, for example contrato.pdf; it also helps to find the file type"`
	AsVoiceNote bool   `json:"as_voice_note,omitempty" jsonschema:"true to convert an audio or video file to a voice note, like one recorded in WhatsApp"`
	ReplyTo     string `json:"reply_to,omitempty" jsonschema:"the id of a message of the same chat to reply to, from get_messages"`
}

type sendMediaOutput struct {
	writeResult
	MessageID string `json:"message_id" jsonschema:"the id of the sent message; pass it to delete_message or react"`
	SentAt    string `json:"sent_at" jsonschema:"the WhatsApp server time, RFC 3339 in UTC"`
	Kind      string `json:"kind" jsonschema:"image, video, audio or document"`
}

func (r *Registry) sendMedia() Tool {
	return Tool{
		Name:         sendMediaName,
		Class:        Write,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        sendMediaName,
				Title:       "Send an image, video, audio, voice note or document",
				Description: "Sends one file from one account to a person or a group. The file comes from exactly one of media_url, upload_id (POST the raw file to /uploads with the same bearer token first) or base64. The file type decides the kind: image, video, audio or document. When the client supports elicitation, the owner sees the account, the destination, the kind, the file name, the size, a short hash and the caption and must accept first. The tool answers after WhatsApp confirms the message.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: accountSchema[sendMediaInput](scope.AccountNames()),
			}, r.handleSendMedia)
		},
	}
}

func (r *Registry) handleSendMedia(ctx context.Context, req *mcp.CallToolRequest, in sendMediaInput) (*mcp.CallToolResult, sendMediaOutput, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return nil, sendMediaOutput{}, err
	}
	src := media.Source{URL: in.MediaURL, UploadID: in.UploadID, Base64: in.Base64, FileName: in.FileName}
	if !src.Single() {
		return nil, sendMediaOutput{}, media.ErrOneSource
	}
	s, err := r.sendTarget(ctx, scope, in.Account, in.To, in.ReplyTo)
	if err != nil {
		return nil, sendMediaOutput{}, err
	}
	file, err := r.deps.Outbox.Load(ctx, scope.Grant.ID, src)
	if err != nil {
		return nil, sendMediaOutput{}, loadError(src, err)
	}
	defer file.Discard()
	plan, err := planFor(file, in)
	if err != nil {
		return nil, sendMediaOutput{}, err
	}
	act := s.action(sendMediaName, plan.question(s), "the media was not sent", s.to.String(), plan.kind, file.Mime, file.Name, strconv.FormatInt(file.Size, 10), file.Hash(), strconv.FormatBool(plan.voiceNote), in.Caption, in.ReplyTo)
	pending, verdict, err := r.approvals.gate(ctx, req, act)
	if pending != nil || err != nil {
		return pending, sendMediaOutput{}, err
	}
	return r.deliverMedia(context.WithoutCancel(ctx), s, plan, verdict)
}

func (r *Registry) deliverMedia(ctx context.Context, s sendTarget, plan mediaPlan, verdict approvalVerdict) (*mcp.CallToolResult, sendMediaOutput, error) {
	ready, err := r.deps.Outbox.Prepare(ctx, plan.file, plan.voiceNote)
	if err != nil {
		return nil, sendMediaOutput{}, fmt.Errorf("the media was not sent: %w", err)
	}
	defer ready.Discard()
	up, err := r.upload(ctx, s, ready, plan.kind)
	if err != nil {
		return nil, sendMediaOutput{}, fmt.Errorf("WhatsApp did not accept the upload, so the media was not sent: %w", err)
	}
	resp, err := s.client.SendMessage(ctx, s.to, plan.message(ready, up, s.quoteContext()))
	if err != nil {
		return nil, sendMediaOutput{}, fmt.Errorf("WhatsApp did not accept the message, so the media was not sent: %w", err)
	}
	msg := s.sentMessage(resp, plan.kind, plan.caption)
	msg.Media = plan.stored(ready, up)
	msg.Media.Path = r.keepSent(s, ready)
	if err := r.deps.Store.SaveMessage(ctx, s.account.ID, s.chat, msg); err != nil {
		r.logUnsaved(s.writeTarget, sendMediaName, errors.Join(err, r.forget(msg.Media.Path)))
	}
	return nil, sendMediaOutput{writeResult: s.result(s.chat, verdict), MessageID: resp.ID, SentAt: timestamp(resp.Timestamp), Kind: plan.kind}, nil
}

func (r *Registry) keepSent(s sendTarget, ready media.Ready) string {
	file, err := ready.Open()
	if err != nil {
		r.logUnkept(s, err)
		return ""
	}
	defer file.Close()
	path, err := r.deps.Media.Files.Keep(s.account.ID, file)
	if err != nil {
		r.logUnkept(s, err)
		return ""
	}
	return path
}

func (r *Registry) logUnkept(s sendTarget, err error) {
	r.deps.Log.Warn("cannot keep a sent file; the server downloads it again from WhatsApp", "account", s.account.Name, "error", err)
}

func (r *Registry) forget(path string) error {
	if path == "" {
		return nil
	}
	return r.deps.Media.Files.Remove(path)
}

func (r *Registry) upload(ctx context.Context, s sendTarget, ready media.Ready, kind string) (whatsmeow.UploadResponse, error) {
	file, err := ready.Open()
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer file.Close()
	scratch, release, err := r.deps.Outbox.Scratch()
	if err != nil {
		return whatsmeow.UploadResponse{}, err
	}
	defer release()
	return s.client.Upload(ctx, file, scratch, whatsmeowKinds[kind])
}

func loadError(src media.Source, err error) error {
	var expired *media.ExpiredUploadError
	var refused *media.FetchError
	switch {
	case errors.As(err, &refused):
		return refused
	case errors.Is(err, media.ErrUnknownUpload):
		return fmt.Errorf("the upload_id %q is unknown for this access; POST the file to /uploads with the same token and pass the new upload_id", src.UploadID)
	case errors.As(err, &expired):
		return fmt.Errorf("the upload_id %q expired at %s; POST the file to /uploads again", src.UploadID, timestamp(expired.ExpiredAt))
	case errors.Is(err, media.ErrBase64TooLarge), errors.Is(err, media.ErrInvalidBase64):
		return err
	}
	return fmt.Errorf("the media was not sent: %w", err)
}

var whatsmeowKinds = map[string]whatsmeow.MediaType{
	media.KindImage:    whatsmeow.MediaImage,
	media.KindVideo:    whatsmeow.MediaVideo,
	media.KindAudio:    whatsmeow.MediaAudio,
	media.KindDocument: whatsmeow.MediaDocument,
}

var storedKinds = map[string]string{
	media.KindImage:    store.MessageImage,
	media.KindVideo:    store.MessageVideo,
	media.KindAudio:    store.MessageAudio,
	media.KindDocument: store.MessageDocument,
}
