package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const fakeMP3 = "ID3 recado de voz"

func TestAnAudioBecomesAVoiceNoteWithItsDuration(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			o := &owner{answer: "accept"}
			session, token := mediaSession(t, h, apptest.WithProtocol(path.protocol), o.elicitation())
			id := h.Upload(t, token, []byte(fakeMP3), "audio/mpeg")

			result := sendMedia(t, session, map[string]any{"to": mariaJID.String(), "upload_id": id, "file_name": "recado.mp3", "as_voice_note": true})

			o.assertAskedOnce(t, fmt.Sprintf("Send this WhatsApp media?\n\nAccount: work (+5511988887777)\nTo: +5531944443333\nKind: voice note, converted from audio/mpeg\nFile: recado.mp3\nSize: %d bytes\nSHA-256: %s\n", len(fakeMP3), shortHash([]byte(fakeMP3))))
			converted := apptest.FakeVoiceNotePrefix + fakeMP3
			audio := h.WhatsApp.Sent(workPhone)[0].Message.GetAudioMessage()
			if string(onlyUpload(t, h)) != converted || !audio.GetPTT() || audio.GetMimetype() != "audio/ogg; codecs=opus" || audio.GetSeconds() != 3 || !strings.Contains(structured(t, result), `"kind":"audio"`) {
				t.Fatalf("WhatsApp got %v", audio)
			}
			messages := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, messages.StructuredContent, `{
				"account": "work",
				"account_phone": "+5511988887777",
				"chat": {"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "phone": "+5531944443333"},
				"messages": [{"id": "3EB0FAKE0001", `+ownMessage+`, "sent_at": "2026-10-13T12:00:00Z", "kind": "audio",
					"media": {"mime": "audio/ogg; codecs=opus", "size": `+fmt.Sprint(len(converted))+`, "seconds": 3, "voice_note": true}, "third_party": {}}]
			}`)
		})
	}
}

func TestAudioAndVideoKeepTheirFormatWithoutVoiceNote(t *testing.T) {
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)

	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString([]byte(fakeMP3)), "file_name": "musica.mp3"})
	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("ftypmp42 video")), "caption": "o gol"})

	sent := h.WhatsApp.Sent(workPhone)
	audio, video := sent[0].Message.GetAudioMessage(), sent[1].Message.GetVideoMessage()
	if audio.GetPTT() || audio.GetMimetype() != "audio/mpeg" || audio.GetSeconds() != 3 || string(h.WhatsApp.Uploads(workPhone)[0].Data) != fakeMP3 {
		t.Fatalf("audio %v", audio)
	}
	if video.GetMimetype() != "video/mp4" || video.GetSeconds() != 3 || video.GetCaption() != "o gol" {
		t.Fatalf("video %v", video)
	}
}

func sineWAV(seconds, rate int) []byte {
	var buf bytes.Buffer
	samples := seconds * rate
	header := []any{[]byte("RIFF"), uint32(36 + samples*2), []byte("WAVEfmt "), uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16), []byte("data"), uint32(samples * 2)}
	for _, field := range header {
		binary.Write(&buf, binary.LittleEndian, field)
	}
	for i := range samples {
		binary.Write(&buf, binary.LittleEndian, int16(8000*math.Sin(2*math.Pi*440*float64(i)/float64(rate))))
	}
	return buf.Bytes()
}

func TestTheRealFFmpegMakesAnOpusVoiceNote(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
	h := apptest.Start(t, apptest.WithRealFFmpeg())
	session, _ := mediaSession(t, h)

	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString(sineWAV(2, 8000)), "file_name": "recado.wav", "as_voice_note": true})

	audio := h.WhatsApp.Sent(workPhone)[0].Message.GetAudioMessage()
	ogg := onlyUpload(t, h)
	if !audio.GetPTT() || audio.GetMimetype() != "audio/ogg; codecs=opus" || audio.GetSeconds() != 2 || !bytes.HasPrefix(ogg, []byte("OggS")) {
		t.Fatalf("WhatsApp got %v", audio)
	}
	path := filepath.Join(t.TempDir(), "sent.ogg")
	if err := os.WriteFile(path, ogg, 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate,channels", "-of", "default=nw=1", path).Output()
	if err != nil || string(probe) != "codec_name=opus\nsample_rate=48000\nchannels=1\n" {
		t.Fatalf("ffprobe: %v %q", err, probe)
	}

	broken := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "base64": base64.StdEncoding.EncodeToString([]byte("not really audio")), "file_name": "x.mp3", "as_voice_note": true}))

	if !broken.IsError || !strings.HasPrefix(errorText(broken), "the media was not sent: ffmpeg failed") || len(h.WhatsApp.Uploads(workPhone)) != 1 {
		t.Fatalf("error %v %q", broken.IsError, errorText(broken))
	}
}
