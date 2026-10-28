package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdimage "image"
	"image/png"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, stdimage.NewGray(stdimage.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

func TestTheOwnerSeesTheMediaBeforeItIsSent(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			o := &owner{answer: "accept"}
			session, token := mediaSession(t, h, apptest.WithProtocol(path.protocol), o.elicitation())
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), text("cadê a foto?")))
			photo := pngOf(t, 2, 3)
			id := h.Upload(t, token, photo, "application/octet-stream")

			result := sendMedia(t, session, map[string]any{"to": mariaJID.String(), "upload_id": id, "file_name": "praia.png", "caption": "olha a praia", "reply_to": "3EB0M1"})

			o.assertAskedOnce(t, fmt.Sprintf("Send this WhatsApp media?\n\nAccount: work (+5511988887777)\nTo: Maria (+5531944443333)\nIn reply to: \"cadê a foto?\", sent by them at 2026-10-13T09:00:00Z\nKind: image (image/png)\nFile: praia.png\nSize: %d bytes\nSHA-256: %s\n\nCaption:\nolha a praia", len(photo), shortHash(photo)))
			if got := structured(t, result); !strings.Contains(got, `"approval":"owner_approved"`) || !strings.Contains(got, `"kind":"image"`) {
				t.Fatalf("result %s", got)
			}
			img := h.WhatsApp.Sent(workPhone)[0].Message.GetImageMessage()
			quote := img.GetContextInfo()
			if img.GetCaption() != "olha a praia" || img.GetMimetype() != "image/png" || img.GetWidth() != 2 || img.GetHeight() != 3 || quote.GetStanzaID() != "3EB0M1" || quote.GetQuotedMessage().GetConversation() != "cadê a foto?" {
				t.Fatalf("WhatsApp got %v", img)
			}
			if !bytes.Equal(onlyUpload(t, h), photo) {
				t.Fatal("WhatsApp got other bytes")
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String(), "after": "3EB0M1"}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "kind": "image", "quoted_id": "3EB0M1",
					"media": {"mime": "image/png", "size": `+fmt.Sprint(len(photo))+`}, "third_party": {"text": "olha a praia"}}],
				"older_before": "3EB0FAKE0001"
			}`)
		})
	}
}

func TestDecliningAMediaSendUploadsNothing(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			o := &owner{answer: "decline"}
			session, token := mediaSession(t, h, apptest.WithProtocol(path.protocol), o.elicitation())
			id := h.Upload(t, token, []byte(contract), "application/pdf")

			result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": id}))

			if !result.IsError || errorText(result) != "the owner did not approve (decline): the media was not sent" {
				t.Fatalf("error %v %q", result.IsError, errorText(result))
			}
			if len(h.WhatsApp.Uploads(workPhone)) != 0 || len(h.WhatsApp.Sent(workPhone)) != 0 {
				t.Fatal("WhatsApp got the media")
			}
		})
	}
}

func TestAnApprovalOfOneFileDoesNotSendAnother(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	token := h.StaticToken(t, "bot", []string{"work"}, false)
	approvedID := h.Upload(t, token, []byte(contract), "application/pdf")
	otherID := h.Upload(t, token, []byte("%PDF-1.7 outro contrato"), "application/pdf")
	asker := h.Connect(t, token, (&owner{answer: "accept"}).elicitation(), apptest.WithoutMultiRoundTrip())
	asked := callTool(t, asker, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": approvedID, "file_name": "contrato.pdf"}))
	if !asked.NeedsInput() {
		t.Fatalf("the first call did not ask the owner: %+v", asked)
	}
	tampered := []map[string]any{
		{"to": mariaJID.String(), "upload_id": otherID, "file_name": "contrato.pdf"},
		{"to": mariaJID.String(), "upload_id": approvedID, "file_name": "contrato.pdf", "caption": "pague hoje"},
		{"to": mariaJID.String(), "upload_id": approvedID, "file_name": "boleto.pdf"},
		{"to": joaoJID.String(), "upload_id": approvedID, "file_name": "contrato.pdf"},
	}

	for _, args := range tampered {
		result := callWith(t, asker, answered("send_media", args, asked.RequestState, "accept"))
		if !result.IsError || !strings.Contains(errorText(result), "the approval does not match this action") {
			t.Fatalf("tampered %v: error %v %q", args, result.IsError, errorText(result))
		}
	}
	if len(h.WhatsApp.Uploads(workPhone)) != 0 || len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatal("WhatsApp got the media")
	}
}

func TestAnApprovalOfAnAudioFileDoesNotSendAVoiceNote(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	token := h.StaticToken(t, "bot", []string{"work"}, false)
	audioID := h.Upload(t, token, []byte(fakeMP3), "audio/mpeg")
	asker := h.Connect(t, token, (&owner{answer: "accept"}).elicitation(), apptest.WithoutMultiRoundTrip())
	approved := map[string]any{"to": mariaJID.String(), "upload_id": audioID, "file_name": "recado.mp3"}
	asked := callTool(t, asker, "send_media", withWork(approved))
	if !asked.NeedsInput() {
		t.Fatalf("the first call did not ask the owner: %+v", asked)
	}

	flipped := map[string]any{"to": mariaJID.String(), "upload_id": audioID, "file_name": "recado.mp3", "as_voice_note": true}
	result := callWith(t, asker, answered("send_media", flipped, asked.RequestState, "accept"))

	if !result.IsError || !strings.Contains(errorText(result), "the approval does not match this action") {
		t.Fatalf("as_voice_note flipped after the approval: error %v %q", result.IsError, errorText(result))
	}
	if len(h.WhatsApp.Sent(workPhone)) != 0 {
		t.Fatal("WhatsApp got the media")
	}
}
