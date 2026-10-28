package media

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const transcodeTimeout = 2 * time.Minute

type Transcoder interface {
	VoiceNote(ctx context.Context, input, output string) error
	Duration(ctx context.Context, path string) (time.Duration, error)
}

type FFmpeg struct{}

func (FFmpeg) VoiceNote(ctx context.Context, input, output string) error {
	_, err := run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", input,
		"-vn", "-sn", "-dn", "-map_metadata", "-1",
		"-ac", "1", "-ar", "48000", "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-application", "voip",
		"-f", "ogg", output)
	return err
}

func (FFmpeg) Duration(ctx context.Context, path string) (time.Duration, error) {
	out, err := run(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe printed no duration: %q", out)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, transcodeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
