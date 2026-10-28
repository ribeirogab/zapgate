package media

import (
	"encoding/hex"
	"os"
	"strings"
)

const shortHashChars = 12

type File struct {
	Path      string
	Size      int64
	SHA256    []byte
	Mime      string
	Name      string
	declared  string
	suggested string
	temporary bool
}

func (f File) Kind() string {
	return kindOf(f.Mime)
}

func (f File) Hash() string {
	return hex.EncodeToString(f.SHA256)
}

func (f File) ShortHash() string {
	return f.Hash()[:shortHashChars]
}

func (f File) Open() (*os.File, error) {
	return os.Open(f.Path)
}

func (f File) Discard() {
	if f.temporary {
		os.Remove(f.Path)
	}
}

func (f File) described(fileName string) (File, error) {
	f.Name = strings.TrimSpace(fileName)
	if f.Name == "" {
		f.Name = f.suggested
	}
	f.Mime = f.declared
	if f.Mime == "" {
		f.Mime = mimeByName(f.Name)
	}
	if f.Mime == "" {
		sniffed, err := f.sniff()
		if err != nil {
			f.Discard()
			return File{}, err
		}
		f.Mime = sniffed
	}
	if f.Name == "" {
		f.Name = nameFor(f.Mime)
	}
	return f, nil
}

func (f File) sniff() (string, error) {
	file, err := f.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	return sniffMime(file), nil
}
