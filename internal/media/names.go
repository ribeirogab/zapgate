package media

import (
	"mime"
	"path"
	"strings"
	"unicode"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	maxFileNameRunes = 120
	fallbackType     = "application/octet-stream"
)

var extensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"video/mp4":       ".mp4",
	"video/3gpp":      ".3gp",
	"audio/ogg":       ".ogg",
	"audio/mpeg":      ".mp3",
	"audio/mp4":       ".m4a",
	"audio/aac":       ".aac",
	"application/pdf": ".pdf",
}

func ContentType(file store.MediaFile) string {
	mediaType, params, err := mime.ParseMediaType(file.Media.Mime)
	if err != nil {
		return fallbackType
	}
	return mime.FormatMediaType(mediaType, params)
}

func contentDisposition(file store.MediaFile) string {
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": DownloadName(file)}); disposition != "" {
		return disposition
	}
	return "attachment"
}

func DownloadName(file store.MediaFile) string {
	if name := cleanFileName(file.Media.FileName); name != "" {
		return name
	}
	mediaType, _, _ := mime.ParseMediaType(file.Media.Mime)
	return file.Kind + "-" + onlyLettersAndDigits(file.MessageID) + extensions[mediaType]
}

func cleanFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' || r == '/' || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if runes := []rune(name); len(runes) > maxFileNameRunes {
		name = string(runes[len(runes)-maxFileNameRunes:])
	}
	return name
}

func onlyLettersAndDigits(value string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return -1
	}, value)
}
