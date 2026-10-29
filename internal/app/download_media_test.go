package app_test

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const mediaWait = 5 * time.Second

var storedFileName = regexp.MustCompile(`^[0-9]+/[0-9a-f]{32}$`)

var photoBytes = []byte("\x89PNG\r\n\x1a\nfoto da nota fiscal")

type cdnFile struct {
	path    string
	key     []byte
	content []byte
}

func newCDNFile(name string, content []byte) cdnFile {
	key := bytes.Repeat([]byte(name[:1]), 32)
	return cdnFile{path: "/v/t62.7118-24/" + name + "?ccb=11-4", key: key, content: content}
}

func (f cdnFile) publish(h *apptest.Harness) cdnFile {
	h.WhatsApp.PublishMedia(f.path, f.key, f.content)
	return f
}

func (f cdnFile) image(caption string) *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String(caption), Mimetype: proto.String("image/png"), FileLength: proto.Uint64(uint64(len(f.content))),
		MediaKey: f.key, DirectPath: proto.String(f.path), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}}
}

func (f cdnFile) document(fileName, mimetype string) *waE2E.Message {
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		FileName: proto.String(fileName), Mimetype: proto.String(mimetype), FileLength: proto.Uint64(uint64(len(f.content))),
		MediaKey: f.key, DirectPath: proto.String(f.path), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}}
}

func mediaFiles(t *testing.T, h *apptest.Harness) map[string][]byte {
	t.Helper()
	found := map[string][]byte{}
	root := filepath.Join(h.DataDir, "media")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == ".partial" {
			return filepath.SkipDir
		}
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		found[strings.TrimPrefix(path, root+string(filepath.Separator))] = content
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return found
}

func awaitMediaFiles(t *testing.T, h *apptest.Harness, count int) map[string][]byte {
	t.Helper()
	deadline := time.Now().Add(mediaWait)
	for {
		found := mediaFiles(t, h)
		if len(found) == count {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("the media dir has %d files, want %d: %v", len(found), count, keysOf(found))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func downloadMedia(t *testing.T, session *mcp.ClientSession, id string) *mcp.CallToolResult {
	t.Helper()
	return callTool(t, session, "download_media", withWork(map[string]any{"chat": mariaJID.String(), "message_id": id}))
}

type downloadedMedia struct {
	Link          string `json:"link"`
	LinkExpiresAt string `json:"link_expires_at"`
}

func mediaLink(t *testing.T, result *mcp.CallToolResult) downloadedMedia {
	t.Helper()
	if result.IsError {
		t.Fatalf("download_media failed: %s", errorText(result))
	}
	var out downloadedMedia
	decode(t, result.StructuredContent, &out)
	return out
}

func inlineImage(result *mcp.CallToolResult) *mcp.ImageContent {
	for _, content := range result.Content {
		if image, ok := content.(*mcp.ImageContent); ok {
			return image
		}
	}
	return nil
}

func withoutLink(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	var out map[string]any
	decode(t, result.StructuredContent, &out)
	delete(out, "link")
	delete(out, "link_expires_at")
	return out
}

func TestAReceivedImageIsDownloadedOnArrivalAndReturnedInline(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	photo := newCDNFile("photo", photoBytes).publish(h)

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), photo.image("nota")))

	for name, content := range awaitMediaFiles(t, h, 1) {
		if !bytes.Equal(content, photoBytes) {
			t.Fatalf("the file %s has %q", name, content)
		}
	}
	result := downloadMedia(t, session, "3EB0P1")
	link := mediaLink(t, result)
	assertJSON(t, withoutLink(t, result), `{`+envelope+`,
		"chat": `+mariaChatRef+`,
		"message_id": "3EB0P1",
		"kind": "image",
		"media": {"mime": "image/png", "size": 27},
		"inline_image": true,
		"third_party": {"text": "nota"}
	}`)
	image := inlineImage(result)
	if image == nil || image.MIMEType != "image/png" || !bytes.Equal(image.Data, photoBytes) {
		t.Fatalf("the inline image is %+v", image)
	}
	if !strings.HasPrefix(link.Link, apptest.PublicURL+"/media/") || link.LinkExpiresAt != "2026-10-13T12:15:00Z" {
		t.Fatalf("link %+v", link)
	}
	if downloads := h.WhatsApp.MediaDownloads(photo.path); len(downloads) != 1 || !bytes.Equal(downloads[0].MediaKey, photo.key) {
		t.Fatalf("WhatsApp saw the downloads %+v", downloads)
	}
}

func TestDownloadMediaOfATextMessageFails(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0T1", "Maria", morning), text("oi")))

	for id, want := range map[string]string{
		"3EB0T1": `the message "3EB0T1" is a text message and has no file`,
		"GHOST":  `the account work has no stored message "GHOST" in the chat 5531944443333@s.whatsapp.net`,
	} {
		result := downloadMedia(t, session, id)
		if !result.IsError || !strings.Contains(errorText(result), want) {
			t.Fatalf("download_media %s: %s", id, errorText(result))
		}
	}
}

func TestAFailedDownloadIsRecordedAndRetriedWhenTheAccountReconnects(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	photo := newCDNFile("photo", photoBytes).publish(h)
	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), photo.image("")))

	result := downloadMedia(t, session, "3EB0P1")
	if !result.IsError || !strings.Contains(errorText(result), "the last attempt failed (the account is not connected to WhatsApp); the server tries again later") {
		t.Fatalf("download_media while offline: %s", errorText(result))
	}
	if files := mediaFiles(t, h); len(files) != 0 {
		t.Fatalf("files %v", files)
	}
	h.WhatsApp.RestoreConnection(workPhone)
	awaitMediaFiles(t, h, 1)
	if downloads := h.WhatsApp.MediaDownloads(photo.path); len(downloads) != 1 || !bytes.Equal(downloads[0].MediaKey, photo.key) || downloads[0].Type != "WhatsApp Image Keys" {
		t.Fatalf("WhatsApp saw the downloads %+v", downloads)
	}
}

func TestAFailedDownloadIsRetriedFromTheStoredKeyAfterARestart(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	doc := newCDNFile("doc", []byte("%PDF-1.7")).publish(h)
	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0D1", "Maria", morning), doc.document("contrato.pdf", "application/pdf")))
	if result := downloadMedia(t, session, "3EB0D1"); !result.IsError {
		t.Fatal("download_media worked offline")
	}

	h.Stop(t)
	h.WhatsApp.RestoreConnection(workPhone)
	h.Resume(t)

	for _, content := range awaitMediaFiles(t, h, 1) {
		if string(content) != "%PDF-1.7" {
			t.Fatalf("the file has %q", content)
		}
	}
	if downloads := h.WhatsApp.MediaDownloads(doc.path); len(downloads) != 1 || !bytes.Equal(downloads[0].MediaKey, doc.key) || downloads[0].Type != "WhatsApp Document Keys" {
		t.Fatalf("WhatsApp saw the downloads %+v", downloads)
	}
}

func TestDownloadMediaTriesAFailedDownloadAgainRightAway(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	photo := newCDNFile("photo", photoBytes).publish(h)
	h.WhatsApp.DropConnection(workPhone)
	h.AwaitAccountState(t, "work", "disconnected")
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0P1", "Maria", morning), photo.image("")))
	if result := downloadMedia(t, session, "3EB0P1"); !result.IsError {
		t.Fatal("download_media worked offline")
	}
	h.WhatsApp.RestoreConnection(workPhone)

	result := downloadMedia(t, session, "3EB0P1")

	if image := inlineImage(result); image == nil || !bytes.Equal(image.Data, photoBytes) {
		t.Fatalf("download_media after the reconnection: %s", errorText(result))
	}
}

func TestAMediaFileWhatsAppNoLongerKeepsIsNotRetried(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	gone := newCDNFile("gone", photoBytes)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0G1", "Maria", morning), gone.image("")))

	for range 2 {
		result := downloadMedia(t, session, "3EB0G1")
		if !result.IsError || !strings.Contains(errorText(result), "the file cannot be downloaded: WhatsApp cannot deliver this file: WhatsApp no longer keeps this media file") {
			t.Fatalf("download_media: %s", errorText(result))
		}
	}
	h.Restart(t)
	time.Sleep(50 * time.Millisecond)
	if downloads := h.WhatsApp.MediaDownloads(gone.path); len(downloads) != 1 {
		t.Fatalf("WhatsApp saw %d downloads", len(downloads))
	}
}

func TestDownloadsRunFourAtATime(t *testing.T) {
	h := apptest.Start(t)
	workSession(t, h)
	h.WhatsApp.HoldMediaDownloads()
	for i := range 10 {
		photo := newCDNFile(string(rune('a'+i))+"photo", photoBytes).publish(h)
		h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0C"+string(rune('A'+i)), "Maria", morning.Add(time.Duration(i)*time.Second)), photo.image("")))
	}

	deadline := time.Now().Add(mediaWait)
	for h.WhatsApp.MediaDownloadsInFlight() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("%d downloads run", h.WhatsApp.MediaDownloadsInFlight())
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if running := h.WhatsApp.MediaDownloadsInFlight(); running != 4 {
		t.Fatalf("%d downloads run at the same time", running)
	}
	h.WhatsApp.ReleaseMediaDownloads()
	awaitMediaFiles(t, h, 10)
}

func TestFilesOfTwoAccountsNeverCollide(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	work := newCDNFile("work", []byte("work file")).publish(h)
	personal := newCDNFile("personal", []byte("personal file")).publish(h)

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "SAME", "Maria", morning), work.document("nota.pdf", "application/pdf")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "SAME", "Maria", morning), personal.document("nota.pdf", "application/pdf")))

	files := awaitMediaFiles(t, h, 2)
	contents := map[string]bool{}
	for name, content := range files {
		if !storedFileName.MatchString(name) {
			t.Fatalf("the stored file name %q is not an account dir and a random name", name)
		}
		contents[string(content)] = true
	}
	if !contents["work file"] || !contents["personal file"] {
		t.Fatalf("files %v", files)
	}
}

func TestRemovingAnAccountDeletesItsMediaFiles(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	session := h.Connect(t, h.StaticToken(t, "bot", []string{"work", "personal"}, false))
	work := newCDNFile("work", []byte("work file")).publish(h)
	personal := newCDNFile("personal", []byte("personal file")).publish(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W1", "Maria", morning), work.document("a.pdf", "application/pdf")))
	h.WhatsApp.Emit(personalPhone, message(direct(mariaJID, "P1", "Maria", morning), personal.document("b.pdf", "application/pdf")))
	awaitMediaFiles(t, h, 2)
	workLink := linkPath(t, mediaLink(t, downloadMedia(t, session, "W1")).Link)

	if result := h.CLI(t, "accounts", "remove", "work", "--yes"); result.Code != 0 {
		t.Fatalf("remove: %+v", result)
	}

	files := mediaFiles(t, h)
	if len(files) != 1 {
		t.Fatalf("files after the removal %v", files)
	}
	for _, content := range files {
		if string(content) != "personal file" {
			t.Fatalf("the file left is %q", content)
		}
	}
	if resp := h.Get(t, workLink); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the link of a removed account answered %d", resp.StatusCode)
	}
}

func keysOf(files map[string][]byte) []string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	return names
}

func TestAMediaFileStopsAfterTwelveFailedAttempts(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	broken := newCDNFile("broken", photoBytes)
	h.WhatsApp.PublishMedia(broken.path, bytes.Repeat([]byte{9}, 32), photoBytes)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0B1", "Maria", morning), broken.image("")))

	for calls := 0; !strings.Contains(errorText(downloadMedia(t, session, "3EB0B1")), "the file cannot be downloaded"); calls++ {
		if calls > 12 {
			t.Fatal("the downloads never stop")
		}
	}
	downloadMedia(t, session, "3EB0B1")

	if downloads := h.WhatsApp.MediaDownloads(broken.path); len(downloads) != 12 {
		t.Fatalf("WhatsApp saw %d downloads", len(downloads))
	}
}
