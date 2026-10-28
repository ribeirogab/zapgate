package app_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func linkPath(t *testing.T, link string) string {
	t.Helper()
	path, ok := strings.CutPrefix(link, apptest.PublicURL)
	if !ok {
		t.Fatalf("the link %q is not under the public URL", link)
	}
	return path
}

func receivedPhotoLink(t *testing.T, h *apptest.Harness) string {
	t.Helper()
	session := workSession(t, h)
	photo := newCDNFile("photo", photoBytes).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), photo.image("nota")))
	return linkPath(t, mediaLink(t, downloadMedia(t, session, "3EB0P1")).Link)
}

func TestTheMediaLinkDownloadsTheFileWithoutTheMCPToken(t *testing.T) {
	h := apptest.Start(t)
	path := receivedPhotoLink(t, h)

	resp := h.Get(t, path)

	body := apptest.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK || body != string(photoBytes) {
		t.Fatalf("status %d, body %q", resp.StatusCode, body)
	}
	want := map[string]string{
		"Content-Type":           "image/png",
		"Content-Disposition":    "attachment; filename=image-3EB0P1.png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, no-store",
	}
	for name, value := range want {
		if got := resp.Header.Get(name); got != value {
			t.Errorf("%s is %q, want %q", name, got, value)
		}
	}
}

func TestTheMediaLinkWorksForFifteenMinutes(t *testing.T) {
	h := apptest.Start(t)
	path := receivedPhotoLink(t, h)

	h.Clock.Advance(15*time.Minute - time.Second)
	if resp := h.Get(t, path); resp.StatusCode != http.StatusOK {
		t.Fatalf("the link failed before it expired: %d", resp.StatusCode)
	}
	h.Clock.Advance(time.Second)
	resp := h.Get(t, path)

	if body := apptest.ReadBody(t, resp); resp.StatusCode != http.StatusGone || strings.Contains(body, "PNG") {
		t.Fatalf("an expired link answered %d with %q", resp.StatusCode, body)
	}
}

func TestATamperedMediaLinkReturnsNoFile(t *testing.T) {
	h := apptest.Start(t)
	path := receivedPhotoLink(t, h)
	payload, signature, _ := strings.Cut(strings.TrimPrefix(path, "/media/"), ".")
	flipped := []byte(signature)
	flipped[0] ^= 1

	for name, tampered := range map[string]string{
		"changed signature": "/media/" + payload + "." + string(flipped),
		"no signature":      "/media/" + payload,
		"other payload":     "/media/" + strings.ToUpper(payload[:4]) + payload[4:] + "." + signature,
		"garbage":           "/media/..%2F..%2Fapp.db",
	} {
		t.Run(name, func(t *testing.T) {
			resp := h.Get(t, tampered)
			if body := apptest.ReadBody(t, resp); resp.StatusCode != http.StatusNotFound || strings.Contains(body, "PNG") {
				t.Fatalf("status %d, body %q", resp.StatusCode, body)
			}
		})
	}
}

func TestADocumentComesOnlyAsALinkWithASafeFileName(t *testing.T) {
	cases := []struct {
		fileName    string
		disposition string
	}{
		{"contrato.pdf", "attachment; filename=contrato.pdf"},
		{"../../app.db", "attachment; filename=app.db"},
		{"nota \"fiscal\"\r\n.pdf", `attachment; filename="nota fiscal.pdf"`},
		{"relatório.pdf", "attachment; filename*=utf-8''relat%C3%B3rio.pdf"},
		{"..", "attachment; filename=document-3EB0D1.pdf"},
	}
	for _, tc := range cases {
		t.Run(tc.fileName, func(t *testing.T) {
			h := apptest.Start(t)
			session := workSession(t, h)
			doc := newCDNFile("doc", []byte("%PDF-1.7 contrato")).publish(h)
			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D1", "Maria", morning), doc.document(tc.fileName, "application/pdf")))

			result := downloadMedia(t, session, "3EB0D1")

			if inlineImage(result) != nil {
				t.Fatal("a document came inline")
			}
			resp := h.Get(t, linkPath(t, mediaLink(t, result).Link))
			if body := apptest.ReadBody(t, resp); body != "%PDF-1.7 contrato" {
				t.Fatalf("status %d, body %q", resp.StatusCode, body)
			}
			if got := resp.Header.Get("Content-Disposition"); got != tc.disposition {
				t.Fatalf("Content-Disposition %q, want %q", got, tc.disposition)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
				t.Fatalf("Content-Type %q", got)
			}
			for name := range mediaFiles(t, h) {
				if strings.Contains(name, "app.db") || strings.Contains(name, "pdf") {
					t.Fatalf("the stored file name %q comes from the sender", name)
				}
			}
		})
	}
}

func TestAnImageAboveFiveMegabytesComesOnlyAsALink(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	large := append(bytes.Repeat([]byte{0x42}, 5<<20), 0x43)
	photo := newCDNFile("large", large).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0L1", "Maria", morning), photo.image("")))

	result := downloadMedia(t, session, "3EB0L1")

	if inlineImage(result) != nil || withoutLink(t, result)["inline_image"] != false {
		t.Fatal("an image above 5 MB came inline")
	}
	resp := h.Get(t, linkPath(t, mediaLink(t, result).Link))
	if body := apptest.ReadBody(t, resp); !bytes.Equal([]byte(body), large) {
		t.Fatalf("the link served %d bytes", len(body))
	}
}

func TestAnImageOfExactlyFiveMegabytesComesInline(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	photo := newCDNFile("exact", bytes.Repeat([]byte{0x42}, 5<<20)).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0E1", "Maria", morning), photo.image("")))

	if image := inlineImage(downloadMedia(t, session, "3EB0E1")); image == nil || len(image.Data) != 5<<20 {
		t.Fatal("an image of 5 MB did not come inline")
	}
}
