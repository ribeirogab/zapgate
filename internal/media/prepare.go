package media

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path"
	"strings"
)

const VoiceNoteMime = "audio/ogg; codecs=opus"

type Ready struct {
	Path      string
	Mime      string
	Name      string
	Seconds   int
	Width     int
	Height    int
	temporary bool
}

func (r Ready) Open() (*os.File, error) {
	return os.Open(r.Path)
}

func (r Ready) Discard() {
	if r.temporary {
		os.Remove(r.Path)
	}
}

func (o *Outbox) Prepare(ctx context.Context, file File, voiceNote bool) (Ready, error) {
	if voiceNote {
		return o.voiceNote(ctx, file)
	}
	ready := Ready{Path: file.Path, Mime: file.Mime, Name: file.Name}
	switch file.Kind() {
	case KindImage:
		ready.Width, ready.Height = imageSize(file.Path)
	case KindVideo, KindAudio:
		ready.Seconds = o.seconds(ctx, file.Path)
	}
	return ready, nil
}

func (o *Outbox) voiceNote(ctx context.Context, file File) (Ready, error) {
	out, err := o.tempFile()
	if err != nil {
		return Ready{}, err
	}
	out.Close()
	ready := Ready{Path: out.Name(), Mime: VoiceNoteMime, Name: strings.TrimSuffix(file.Name, path.Ext(file.Name)) + ".ogg", temporary: true}
	if err := o.codec.VoiceNote(ctx, file.Path, ready.Path); err != nil {
		ready.Discard()
		return Ready{}, err
	}
	ready.Seconds = o.seconds(ctx, ready.Path)
	return ready, nil
}

func (o *Outbox) seconds(ctx context.Context, path string) int {
	duration, err := o.codec.Duration(ctx, path)
	if err != nil {
		o.log.Warn("cannot read the duration of a media file", "error", err)
		return 0
	}
	return max(1, int(math.Round(duration.Seconds())))
}

func imageSize(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
