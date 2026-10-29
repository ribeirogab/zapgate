package media

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	MaxBase64Bytes = 5 << 20
	dataURLPrefix  = "data:"
	base64Marker   = ";base64,"
)

var (
	ErrOneSource      = errors.New("pass exactly one of media_url, upload_id or base64")
	ErrBase64TooLarge = fmt.Errorf("the base64 file is larger than %d MB; POST it to /uploads and pass the upload_id", MaxBase64Bytes>>20)
	ErrInvalidBase64  = errors.New("the base64 value is not valid base64")
)

var base64Encodings = []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}

type Source struct {
	URL      string
	UploadID string
	Base64   string
	FileName string
}

func (s Source) Single() bool {
	return s.count() == 1
}

func (s Source) count() int {
	n := 0
	for _, value := range []string{s.URL, s.UploadID, s.Base64} {
		if value != "" {
			n++
		}
	}
	return n
}

func (o *Outbox) Load(ctx context.Context, grantID int64, src Source) (File, error) {
	var (
		file File
		err  error
	)
	switch {
	case !src.Single():
		return File{}, ErrOneSource
	case src.UploadID != "":
		file, err = o.openUpload(grantID, src.UploadID)
	case src.Base64 != "":
		file, err = o.decode(src.Base64)
	default:
		file, err = o.fetch(ctx, src.URL)
	}
	if err != nil {
		return File{}, err
	}
	return file.described(src.FileName)
}

func (o *Outbox) decode(value string) (File, error) {
	declared, encoded := splitDataURL(value)
	encoded = strings.Join(strings.Fields(encoded), "")
	if base64.RawStdEncoding.DecodedLen(len(encoded)) > MaxBase64Bytes+2 {
		return File{}, ErrBase64TooLarge
	}
	data, err := decodeAny(encoded)
	switch {
	case err != nil:
		return File{}, err
	case len(data) > MaxBase64Bytes:
		return File{}, ErrBase64TooLarge
	case len(data) == 0:
		return File{}, ErrEmpty
	}
	return o.keep(data, declared)
}

func splitDataURL(value string) (string, string) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, dataURLPrefix) {
		return "", value
	}
	header, encoded, ok := strings.Cut(strings.TrimPrefix(value, dataURLPrefix), base64Marker)
	if !ok {
		return "", value
	}
	return declaredMime(header), encoded
}

func decodeAny(encoded string) ([]byte, error) {
	for _, encoding := range base64Encodings {
		if data, err := encoding.DecodeString(encoded); err == nil {
			return data, nil
		}
	}
	return nil, ErrInvalidBase64
}

func (o *Outbox) keep(data []byte, declared string) (File, error) {
	temp, err := o.tempFile()
	if err != nil {
		return File{}, err
	}
	_, err = temp.Write(data)
	if err := errors.Join(err, temp.Close()); err != nil {
		os.Remove(temp.Name())
		return File{}, err
	}
	sum := sha256.Sum256(data)
	return File{Path: temp.Name(), Size: int64(len(data)), SHA256: sum[:], declared: declared, temporary: true}, nil
}
