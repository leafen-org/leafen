//go:build android

package main

/*
#include <stdlib.h>

int leafenFoldersReady(void);
void leafenFoldersChoose(int callbackID);
char *leafenFoldersText(const char *name, const char *tree, const char *path);
int leafenFoldersAct(const char *name, const char *tree, const char *path, const char *target);
int leafenFoldersOpen(const char *name, const char *tree, const char *path, int create);
*/
import "C"

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"leafen.org/leafen/internal/app"
)

// On Android a backup folder is picked with the system's document-tree
// picker and kept with a persisted permission (LeafenBackupFolders.java):
// the folder's name is "android-folder:" with its shown name and the tree's
// address, and the server reaches it through the helper.

const androidFolderPrefix = "android-folder:"

var (
	androidFolderChoices sync.Map // ID → chan string
	androidFolderCount   atomic.Int32
	// A refusal is the picked folder's access gone (or the folder): the
	// backup says it can't reach the folder, to be picked again.
	errAndroidFolder = fmt.Errorf("the phone's document provider refused the backup folder's change: %w", fs.ErrPermission)
)

// chooseFolder shows the document-tree picker and answers the folder's
// name, or "" when the person cancelled.
func chooseFolder(string) (string, error) {
	if C.leafenFoldersReady() == 0 {
		return "", errNoFolderDialog
	}
	id := androidFolderCount.Add(1)
	chosen := make(chan string, 1)
	androidFolderChoices.Store(id, chosen)
	defer androidFolderChoices.Delete(id)
	C.leafenFoldersChoose(C.int(id))
	return <-chosen, nil
}

//export leafenAndroidFolderChosen
func leafenAndroidFolderChosen(callbackID C.int, tree *C.char, name *C.char) {
	waiting, ok := androidFolderChoices.Load(int32(callbackID))
	if !ok {
		return
	}
	folder := ""
	if address := C.GoString(tree); address != "" {
		folder = androidFolderPrefix + url.QueryEscape(C.GoString(name)) + ":" + address
	}
	waiting.(chan string) <- folder
}

func registerDeviceBackupFolders() {
	app.SetDeviceBackupFolders(func(name string) (app.DeviceBackupFolder, bool, error) {
		rest, ok := strings.CutPrefix(name, androidFolderPrefix)
		if !ok {
			return nil, false, nil
		}
		_, tree, found := strings.Cut(rest, ":")
		if !found || !strings.HasPrefix(tree, "content://") {
			return nil, true, errors.New("the folder's address is missing")
		}
		if C.leafenFoldersReady() == 0 {
			return nil, true, errors.New("the phone's document providers aren't reachable yet")
		}
		return androidBackupFolder{tree: tree}, true, nil
	})
}

// androidBackupFolder is a picked document tree, reached through the helper.
type androidBackupFolder struct{ tree string }

func (folder androidBackupFolder) text(method, relative string) (string, bool) {
	name, tree, target := C.CString(method), C.CString(folder.tree), C.CString(relative)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(tree))
	defer C.free(unsafe.Pointer(target))
	answer := C.leafenFoldersText(name, tree, target)
	if answer == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(answer))
	return C.GoString(answer), true
}

func (folder androidBackupFolder) act(method, relative, to string, renaming bool) bool {
	name, tree, target := C.CString(method), C.CString(folder.tree), C.CString(relative)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(tree))
	defer C.free(unsafe.Pointer(target))
	var destination *C.char
	if renaming {
		destination = C.CString(to)
		defer C.free(unsafe.Pointer(destination))
	}
	return C.leafenFoldersAct(name, tree, target, destination) == 1
}

func (folder androidBackupFolder) open(method, relative string, create bool) (*os.File, error) {
	name, tree, target := C.CString(method), C.CString(folder.tree), C.CString(relative)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(tree))
	defer C.free(unsafe.Pointer(target))
	flag := C.int(0)
	if create {
		flag = 1
	}
	switch descriptor := int(C.leafenFoldersOpen(name, tree, target, flag)); {
	case descriptor >= 0:
		return os.NewFile(uintptr(descriptor), path.Base(relative)), nil
	case descriptor == -1:
		return nil, fs.ErrNotExist
	case descriptor == -2:
		return nil, fs.ErrExist
	default:
		return nil, errAndroidFolder
	}
}

type androidEntry struct {
	Name     string `json:"name"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

func (entry androidEntry) backup() app.DeviceBackupEntry {
	modified := time.Time{}
	if entry.Modified > 0 {
		modified = time.UnixMilli(entry.Modified)
	}
	return app.DeviceBackupEntry{Name: entry.Name, Dir: entry.Dir, Size: entry.Size, Modified: modified}
}

func (folder androidBackupFolder) MakeDir(relative string) error {
	if !folder.act("makeDir", relative, "", false) {
		return errAndroidFolder
	}
	return nil
}

// Create writes beside the target and renames it into place once written,
// so a cut-off write leaves nothing under the name.
func (folder androidBackupFolder) Create(relative string) (io.WriteCloser, error) {
	if _, err := folder.Stat(relative); err == nil {
		return nil, fs.ErrExist
	}
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	incoming := path.Join(path.Dir(relative), ".incoming-"+hex.EncodeToString(random))
	file, err := folder.open("openWrite", incoming, true)
	if err != nil {
		return nil, err
	}
	return &androidBackupWriter{folder: folder, file: file, incoming: incoming, target: relative}, nil
}

type androidBackupWriter struct {
	folder           androidBackupFolder
	file             *os.File
	incoming, target string
}

func (writer *androidBackupWriter) Write(data []byte) (int, error) { return writer.file.Write(data) }

func (writer *androidBackupWriter) Close() error {
	if err := errors.Join(writer.file.Sync(), writer.file.Close()); err != nil {
		_ = writer.folder.RemoveAll(writer.incoming)
		return err
	}
	if err := writer.folder.Rename(writer.incoming, writer.target); err != nil {
		_ = writer.folder.RemoveAll(writer.incoming)
		return err
	}
	return nil
}

func (folder androidBackupFolder) Open(relative string) (io.ReadCloser, error) {
	return folder.open("openRead", relative, false)
}

func (folder androidBackupFolder) Stat(relative string) (app.DeviceBackupEntry, error) {
	answer, ok := folder.text("stat", relative)
	if !ok {
		return app.DeviceBackupEntry{}, fs.ErrNotExist
	}
	entry := androidEntry{}
	if err := json.Unmarshal([]byte(answer), &entry); err != nil {
		return app.DeviceBackupEntry{}, err
	}
	return entry.backup(), nil
}

func (folder androidBackupFolder) Rename(from, to string) error {
	if _, err := folder.Stat(to); err == nil {
		return fs.ErrExist
	}
	if !folder.act("rename", from, to, true) {
		if _, err := folder.Stat(from); errors.Is(err, fs.ErrNotExist) {
			return fs.ErrNotExist
		}
		return errAndroidFolder
	}
	return nil
}

func (folder androidBackupFolder) List(relative string) ([]app.DeviceBackupEntry, error) {
	answer, ok := folder.text("list", relative)
	if !ok {
		return nil, fs.ErrNotExist
	}
	entries := []androidEntry{}
	if err := json.Unmarshal([]byte(answer), &entries); err != nil {
		return nil, err
	}
	listed := make([]app.DeviceBackupEntry, 0, len(entries))
	for _, entry := range entries {
		listed = append(listed, entry.backup())
	}
	return listed, nil
}

func (folder androidBackupFolder) RemoveAll(relative string) error {
	if !folder.act("removeAll", relative, "", false) {
		return errAndroidFolder
	}
	return nil
}

func (folder androidBackupFolder) Replace(relative string, content []byte) error {
	file, err := folder.open("openWrite", relative, false)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	return errors.Join(writeErr, file.Close())
}

func (androidBackupFolder) Close() error { return nil }
