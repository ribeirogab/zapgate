package media

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

const (
	KindImage    = "image"
	KindVideo    = "video"
	KindAudio    = "audio"
	KindDocument = "document"
	sniffBytes   = 512
	defaultName  = "file"
)

var meaninglessTypes = map[string]bool{
	"":                                  true,
	"application/octet-stream":          true,
	"application/x-www-form-urlencoded": true,
	"binary/octet-stream":               true,
}

type knownType struct {
	ext  string
	mime string
}

var knownTypes = []knownType{
	{ext: ".jpg", mime: "image/jpeg"},
	{ext: ".jpeg", mime: "image/jpeg"},
	{ext: ".png", mime: "image/png"},
	{ext: ".webp", mime: "image/webp"},
	{ext: ".gif", mime: "image/gif"},
	{ext: ".mp4", mime: "video/mp4"},
	{ext: ".m4v", mime: "video/mp4"},
	{ext: ".3gp", mime: "video/3gpp"},
	{ext: ".mov", mime: "video/quicktime"},
	{ext: ".webm", mime: "video/webm"},
	{ext: ".mp3", mime: "audio/mpeg"},
	{ext: ".ogg", mime: "audio/ogg"},
	{ext: ".oga", mime: "audio/ogg"},
	{ext: ".opus", mime: "audio/ogg"},
	{ext: ".m4a", mime: "audio/mp4"},
	{ext: ".aac", mime: "audio/aac"},
	{ext: ".amr", mime: "audio/amr"},
	{ext: ".wav", mime: "audio/wav"},
	{ext: ".pdf", mime: "application/pdf"},
	{ext: ".txt", mime: "text/plain"},
	{ext: ".csv", mime: "text/csv"},
	{ext: ".zip", mime: "application/zip"},
	{ext: ".doc", mime: "application/msword"},
	{ext: ".docx", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	{ext: ".xls", mime: "application/vnd.ms-excel"},
	{ext: ".xlsx", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	{ext: ".ppt", mime: "application/vnd.ms-powerpoint"},
	{ext: ".pptx", mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
}

var sniffAliases = map[string]string{
	"application/ogg": "audio/ogg",
	"audio/wave":      "audio/wav",
}

var kindsByType = map[string]string{
	"image/jpeg": KindImage,
	"image/png":  KindImage,
	"image/webp": KindImage,
	"video/mp4":  KindVideo,
	"video/3gpp": KindVideo,
	"audio/ogg":  KindAudio,
	"audio/mpeg": KindAudio,
	"audio/mp4":  KindAudio,
	"audio/aac":  KindAudio,
	"audio/amr":  KindAudio,
}

func declaredMime(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(value))
	}
	if meaninglessTypes[mediaType] {
		return ""
	}
	return mediaType
}

func mimeByName(name string) string {
	ext := strings.ToLower(path.Ext(name))
	for _, known := range knownTypes {
		if known.ext == ext {
			return known.mime
		}
	}
	return declaredMime(mime.TypeByExtension(ext))
}

func sniffMime(r io.Reader) string {
	head := make([]byte, sniffBytes)
	n, _ := io.ReadFull(r, head)
	sniffed := declaredMime(http.DetectContentType(head[:n]))
	if alias, ok := sniffAliases[sniffed]; ok {
		return alias
	}
	if sniffed == "" {
		return "application/octet-stream"
	}
	return sniffed
}

func kindOf(mediaType string) string {
	if kind, ok := kindsByType[mediaType]; ok {
		return kind
	}
	return KindDocument
}

func Convertible(mediaType string) bool {
	return strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/")
}

func nameFor(mediaType string) string {
	for _, known := range knownTypes {
		if known.mime == mediaType {
			return defaultName + known.ext
		}
	}
	if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		return defaultName + exts[0]
	}
	return defaultName
}
