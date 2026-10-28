package apptest

import (
	"context"
	"os"
	"time"
)

const (
	FakeVoiceNotePrefix = "OggS fake voice note of "
	FakeMediaDuration   = 3400 * time.Millisecond
)

type fakeTranscoder struct{}

func (fakeTranscoder) VoiceNote(_ context.Context, input, output string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	return os.WriteFile(output, append([]byte(FakeVoiceNotePrefix), data...), 0o600)
}

func (fakeTranscoder) Duration(context.Context, string) (time.Duration, error) {
	return FakeMediaDuration, nil
}
