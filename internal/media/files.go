package media

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
)

const (
	dirMode     = 0o700
	fileMode    = 0o600
	partialDir  = ".partial"
	nameBytes   = 16
	partialFlag = os.O_RDWR | os.O_CREATE | os.O_EXCL
)

type Files struct {
	root *os.Root
}

func OpenFiles(dir string) (*Files, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create the media dir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open the media dir: %w", err)
	}
	files := &Files{root: root}
	if err := files.resetPartials(); err != nil {
		root.Close()
		return nil, err
	}
	return files, nil
}

func (f *Files) Close() error {
	return f.root.Close()
}

func (f *Files) Open(name string) (*os.File, error) {
	return f.root.Open(name)
}

func (f *Files) Remove(name string) error {
	return ignoreMissing(f.root.Remove(name))
}

func (f *Files) Keep(accountID int64, content io.Reader) (string, error) {
	part, err := f.createPartial()
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, content); err != nil {
		part.discard()
		return "", err
	}
	return part.keep(accountID)
}

func (f *Files) RemoveAccount(accountID int64) error {
	return f.root.RemoveAll(accountDir(accountID))
}

func (f *Files) AccountBytes(accountID int64) (int64, error) {
	var total int64
	err := fs.WalkDir(f.root.FS(), accountDir(accountID), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return ignoreMissing(err)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return ignoreMissing(err)
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func (f *Files) resetPartials() error {
	if err := f.root.RemoveAll(partialDir); err != nil {
		return fmt.Errorf("clean the partial downloads: %w", err)
	}
	return f.root.Mkdir(partialDir, dirMode)
}

type partial struct {
	*os.File
	files *Files
	name  string
}

func (f *Files) createPartial() (*partial, error) {
	name := randomName()
	file, err := f.root.OpenFile(path.Join(partialDir, name), partialFlag, fileMode)
	if err != nil {
		return nil, err
	}
	return &partial{File: file, files: f, name: name}, nil
}

func (p *partial) discard() {
	p.Close()
	p.files.root.Remove(path.Join(partialDir, p.name))
}

func (p *partial) keep(accountID int64) (string, error) {
	if err := p.Close(); err != nil {
		p.discard()
		return "", err
	}
	dir := accountDir(accountID)
	if err := p.files.root.MkdirAll(dir, dirMode); err != nil {
		p.discard()
		return "", err
	}
	final := path.Join(dir, p.name)
	if err := p.files.root.Rename(path.Join(partialDir, p.name), final); err != nil {
		p.discard()
		return "", err
	}
	return final, nil
}

func accountDir(accountID int64) string {
	return strconv.FormatInt(accountID, 10)
}

func randomName() string {
	name := make([]byte, nameBytes)
	rand.Read(name)
	return hex.EncodeToString(name)
}
