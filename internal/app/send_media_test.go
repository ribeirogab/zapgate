package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const contract = "%PDF-1.7 contrato"

func mediaSession(t *testing.T, h *apptest.Harness, opts ...apptest.ConnectOption) (*mcp.ClientSession, string) {
	t.Helper()
	h.AddAccount(t, "work", workPhone, "Alex")
	token := h.StaticToken(t, "bot", []string{"work"}, false)
	return h.Connect(t, token, opts...), token
}

func sendMedia(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result := callTool(t, session, "send_media", withWork(args))
	if result.IsError {
		t.Fatalf("send_media %v failed: %s", args, errorText(result))
	}
	return result
}

func onlyUpload(t *testing.T, h *apptest.Harness) []byte {
	t.Helper()
	uploads := h.WhatsApp.Uploads(workPhone)
	if len(uploads) != 1 {
		t.Fatalf("WhatsApp got %d uploads", len(uploads))
	}
	return uploads[0].Data
}

func TestAnUploadedDocumentIsSentAndAppearsInGetMessages(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			session, token := mediaSession(t, h, apptest.WithProtocol(path.protocol))
			id := h.Upload(t, token, []byte(contract), "application/pdf")

			result := sendMedia(t, session, map[string]any{"to": "+5531944443333", "upload_id": id, "file_name": "contrato.pdf", "caption": "segue o contrato"})

			assertJSON(t, result.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
				"message_id": "3EB0FAKE0001",
				"sent_at": "2026-10-13T12:00:00Z",
				"kind": "document",
				"approval": "client_without_elicitation"
			}`)
			uploads := h.WhatsApp.Uploads(workPhone)
			if len(uploads) != 1 || string(uploads[0].Data) != contract || uploads[0].Kind != whatsmeow.MediaDocument {
				t.Fatalf("WhatsApp got the uploads %+v", uploads)
			}
			up := uploads[0].Response
			sent := h.WhatsApp.Sent(workPhone)
			doc := sent[0].Message.GetDocumentMessage()
			hash := sha256.Sum256([]byte(contract))
			if len(sent) != 1 || sent[0].To != mariaJID || doc.GetFileName() != "contrato.pdf" || doc.GetTitle() != "contrato.pdf" || doc.GetMimetype() != "application/pdf" || doc.GetCaption() != "segue o contrato" ||
				doc.GetURL() != up.URL || doc.GetDirectPath() != up.DirectPath || !bytes.Equal(doc.GetMediaKey(), up.MediaKey) ||
				!bytes.Equal(doc.GetFileSHA256(), hash[:]) || !bytes.Equal(doc.GetFileEncSHA256(), up.FileEncSHA256) || doc.GetFileLength() != uint64(len(contract)) {
				t.Fatalf("WhatsApp got %v", sent)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "kind": "document",
					"media": {"mime": "application/pdf", "size": 17}, "third_party": {"text": "segue o contrato", "file_name": "contrato.pdf"}}]
			}`)
		})
	}
}

func TestASentFileIsKeptSoDownloadMediaNeedsNoDownload(t *testing.T) {
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)
	photo := pngOf(t, 3, 3)

	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString(photo)})

	files := mediaFiles(t, h)
	for name, content := range files {
		if !storedFileName.MatchString(filepath.ToSlash(name)) || !bytes.Equal(content, photo) {
			t.Fatalf("the media dir holds %s with %d bytes", name, len(content))
		}
	}
	result := downloadMedia(t, session, "3EB0FAKE0001")
	if image := inlineImage(result); len(files) != 1 || image == nil || !bytes.Equal(image.Data, photo) {
		t.Fatalf("files %v, download_media %s", keysOf(files), errorText(result))
	}
	time.Sleep(50 * time.Millisecond)
	if downloads := h.WhatsApp.MediaDownloads(h.WhatsApp.Uploads(workPhone)[0].Response.DirectPath); len(downloads) != 0 {
		t.Fatalf("the server downloaded its own file back: %+v", downloads)
	}
}

func TestWhenWhatsAppRefusesTheUploadNothingIsStoredOrKept(t *testing.T) {
	h := apptest.Start(t)
	session, token := mediaSession(t, h)
	id := h.Upload(t, token, []byte(contract), "application/pdf")
	h.WhatsApp.FailSends(workPhone, errors.New("media upload failed: 500"))

	result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": id}))

	if want := "WhatsApp did not accept the upload, so the media was not sent: media upload failed: 500"; !result.IsError || errorText(result) != want {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
	if files := mediaFiles(t, h); len(files) != 0 {
		t.Fatalf("the media dir holds %v", keysOf(files))
	}
	if again := callTool(t, session, "get_messages", withWork(map[string]any{"chat": mariaJID.String()})); !again.IsError {
		t.Fatalf("a chat was stored: %s", structured(t, again))
	}
}
