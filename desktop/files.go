package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// FileSaver saves a file the window hands over onto the device, as a page a
// browser shows would download it: where the person picks on a desktop,
// through the share sheet on iOS (Android's window has the app's own
// Downloads store, LeafenFiles.java). The window sends the file in pieces,
// as Wails' bindings carry text.
type FileSaver struct {
	mu     sync.Mutex
	saving map[string]*savingFile
}

type savingFile struct {
	file *os.File
	path string
}

const maxFileChunkChars = 8 << 20

var errUnknownSave = errors.New("no file is being saved under that name")

func newFileSaver() *FileSaver {
	return &FileSaver{saving: map[string]*savingFile{}}
}

// savedFileName keeps a suggested name to a plain file name.
func savedFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

// Begin asks where the file goes and opens it there, giving the save's ID,
// or "" when the person cancelled.
func (saver *FileSaver) Begin(name string) (string, error) {
	path, err := chooseSavePath(savedFileName(name))
	if err != nil || path == "" {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	id := hex.EncodeToString(random)
	saver.mu.Lock()
	saver.saving[id] = &savingFile{file: file, path: path}
	saver.mu.Unlock()
	return id, nil
}

func (saver *FileSaver) take(id string) *savingFile {
	saver.mu.Lock()
	defer saver.mu.Unlock()
	return saver.saving[id]
}

// Append writes the next piece, in base64.
func (saver *FileSaver) Append(id, chunk string) error {
	saving := saver.take(id)
	if saving == nil {
		return errUnknownSave
	}
	if len(chunk) > maxFileChunkChars {
		return errors.New("the piece is too large")
	}
	bytes, err := base64.StdEncoding.DecodeString(chunk)
	if err != nil {
		return err
	}
	_, err = saving.file.Write(bytes)
	return err
}

// Finish closes the file where it was saved, giving where that is.
func (saver *FileSaver) Finish(id string) (string, error) {
	saver.mu.Lock()
	saving := saver.saving[id]
	delete(saver.saving, id)
	saver.mu.Unlock()
	if saving == nil {
		return "", errUnknownSave
	}
	if err := errors.Join(saving.file.Sync(), saving.file.Close()); err != nil {
		_ = os.Remove(saving.path)
		return "", err
	}
	return saving.path, finishSave(saving.path)
}

// Abort drops a save the window couldn't finish.
func (saver *FileSaver) Abort(id string) {
	saver.mu.Lock()
	saving := saver.saving[id]
	delete(saver.saving, id)
	saver.mu.Unlock()
	if saving != nil {
		_ = saving.file.Close()
		_ = os.Remove(saving.path)
	}
}
