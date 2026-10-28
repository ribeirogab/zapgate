package app_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const maxBase64Bytes = 5 << 20

func TestABase64FileIsSentWithTheTypeOfItsContent(t *testing.T) {
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)
	photo := pngOf(t, 4, 4)

	image := sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString(photo)})
	doc := sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": "data:application/pdf;base64," + base64.RawURLEncoding.EncodeToString([]byte(contract))})

	if !strings.Contains(structured(t, image), `"kind":"image"`) || !strings.Contains(structured(t, doc), `"kind":"document"`) {
		t.Fatalf("results %s and %s", structured(t, image), structured(t, doc))
	}
	uploads := h.WhatsApp.Uploads(workPhone)
	sent := h.WhatsApp.Sent(workPhone)
	if len(uploads) != 2 || !bytes.Equal(uploads[0].Data, photo) || string(uploads[1].Data) != contract {
		t.Fatalf("WhatsApp got the uploads %+v", uploads)
	}
	if sent[0].Message.GetImageMessage().GetMimetype() != "image/png" || sent[1].Message.GetDocumentMessage().GetMimetype() != "application/pdf" || sent[1].Message.GetDocumentMessage().GetFileName() != "file.pdf" {
		t.Fatalf("WhatsApp got %v", sent)
	}
}

func TestBase64TakesUpTo5MB(t *testing.T) {
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)
	limit := bytes.Repeat([]byte{'a'}, maxBase64Bytes)

	accepted := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString(limit), "file_name": "notas.txt"}))
	refused := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString(append(limit, 'a'))}))

	if accepted.IsError {
		t.Fatalf("5 MB: %s", errorText(accepted))
	}
	if want := "the base64 file is larger than 5 MB; POST it to /uploads and pass the upload_id"; !refused.IsError || errorText(refused) != want {
		t.Fatalf("5 MB + 1: error %v %q", refused.IsError, errorText(refused))
	}
	if doc := h.WhatsApp.Sent(workPhone)[0].Message.GetDocumentMessage(); doc.GetMimetype() != "text/plain" || doc.GetFileLength() != maxBase64Bytes {
		t.Fatalf("WhatsApp got %v", doc)
	}
}

func TestSendMediaNeedsExactlyOneValidSource(t *testing.T) {
	h := apptest.Start(t)
	session, token := mediaSession(t, h)
	id := h.Upload(t, token, []byte(contract), "application/pdf")
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"none":     {map[string]any{}, "pass exactly one of media_url, upload_id or base64"},
		"two":      {map[string]any{"upload_id": id, "base64": "aGk="}, "pass exactly one of media_url, upload_id or base64"},
		"bad":      {map[string]any{"base64": "not base64!"}, "the base64 value is not valid base64"},
		"empty":    {map[string]any{"base64": "data:image/png;base64,"}, "the media was not sent: the file is empty"},
		"caption":  {map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte("ID3 audio")), "file_name": "a.mp3", "caption": "ouça"}, "WhatsApp shows no caption on audio; send the caption with send_message"},
		"not_song": {map[string]any{"upload_id": id, "as_voice_note": true}, "as_voice_note needs an audio or video file, and this file is application/pdf; send it without as_voice_note"},
	}

	for name, c := range cases {
		c.args["to"] = mariaJID.String()
		result := callTool(t, session, "send_media", withWork(c.args))
		if !result.IsError || errorText(result) != c.want {
			t.Errorf("%s: error %v %q, want %q", name, result.IsError, errorText(result), c.want)
		}
	}
	if len(h.WhatsApp.Uploads(workPhone)) != 0 {
		t.Fatal("WhatsApp got an upload")
	}
}
