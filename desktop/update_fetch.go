//go:build darwin && !ios

package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"leafen.org/leafen/internal/app"
)

// Fetching a new version for the Mac's updater (updater_darwin.go): the file
// the list names, checked by size and SHA-256, unpacked, and checked against
// macOS's own checks of who made it. Windows fetches no version
// (updater_windows.go).

// fetchVerified downloads a file and checks its size and SHA-256 against
// the list, telling `received` the bytes it has so far as they come.
func fetchVerified(ctx context.Context, client *http.Client, asset *app.DesktopAsset, path string, received func(int64)) error {
	download := *client
	download.Timeout = 30 * time.Minute
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	response, err := download.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the new version didn't download (%d)", response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest, &byteCount{report: received}), io.LimitReader(response.Body, min(asset.Size, updateMaxBytes)+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if written != asset.Size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), asset.SHA256) {
		return errors.New("the new version's file isn't the one the list names")
	}
	return nil
}

// byteCount tells how many bytes have gone through it so far.
type byteCount struct {
	total  int64
	report func(int64)
}

func (count *byteCount) Write(chunk []byte) (int, error) {
	count.total += int64(len(chunk))
	if count.report != nil {
		count.report(count.total)
	}
	return len(chunk), nil
}

// unpackUpdate opens a downloaded zip into the new app's bundle.
func unpackUpdate(download, into string) (string, error) {
	if err := os.MkdirAll(into, 0o700); err != nil {
		return "", err
	}
	archive, err := zip.OpenReader(download)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	top := ""
	// Links are made last, so no file is written through one, and only to
	// places inside the app.
	type link struct{ path, target string }
	links := []link{}
	inside := filepath.Clean(into) + string(os.PathSeparator)
	for _, entry := range archive.File {
		// The Finder's and ditto's zips keep file metadata beside the app.
		if strings.HasPrefix(entry.Name, "__MACOSX/") || strings.HasPrefix(filepath.Base(entry.Name), "._") {
			continue
		}
		name := filepath.Clean(entry.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return "", errors.New("the new version's zip leaves its folder")
		}
		first := strings.Split(filepath.ToSlash(name), "/")[0]
		if top == "" {
			top = first
		} else if top != first {
			return "", errors.New("the new version's zip holds more than one app")
		}
		path := filepath.Join(into, name)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			reader, err := entry.Open()
			if err != nil {
				return "", err
			}
			target, err := io.ReadAll(io.LimitReader(reader, 4096))
			reader.Close()
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(string(target)) || !strings.HasPrefix(filepath.Join(filepath.Dir(path), string(target)), inside) {
				return "", errors.New("the new version's zip links outside its folder")
			}
			links = append(links, link{path: path, target: string(target)})
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return "", err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, entry.Mode().Perm())
		if err == nil {
			_, err = io.Copy(file, reader)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		reader.Close()
		if err != nil {
			return "", err
		}
	}
	if top == "" {
		return "", errors.New("the new version's zip is empty")
	}
	for _, each := range links {
		if err := os.Symlink(each.target, each.path); err != nil {
			return "", err
		}
	}
	return filepath.Join(into, top), nil
}

// leafenMacPublisher is the code requirement a new version from Leafen's
// lists meets on macOS: a Developer ID Application signature of Leafen's own
// team, the requirement macOS itself writes for such an app. Notarization
// alone takes an app any developer had notarized.
const leafenMacPublisher = `anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists` +
	` and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "C2J8SUQ5M5"`

// verifyPublisher runs macOS's own checks of who made the new app. It is
// the second lock beside the list's: the list is signed by the release key
// and names each file's SHA-256 (app.ReadDesktopRelease), and the file must
// also be signed by Leafen's team and notarized, so neither a stolen release
// key nor a file put up beside a release alone changes the app. Lists from a
// development address skip the team and notarization, which only Leafen's
// published builds carry.
func verifyPublisher(payload string) error {
	if output, err := exec.Command("codesign", "--verify", "--deep", "--strict", payload).CombinedOutput(); err != nil {
		return fmt.Errorf("the new version's signature doesn't hold: %s", strings.TrimSpace(string(output)))
	}
	if app.DesktopReleaseFromDevelopmentList() {
		return nil
	}
	if output, err := exec.Command("codesign", "--verify", "-R="+leafenMacPublisher, payload).CombinedOutput(); err != nil {
		return fmt.Errorf("the new version isn't signed by Leafen: %s", strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("spctl", "--assess", "--type", "execute", payload).CombinedOutput(); err != nil {
		return fmt.Errorf("the new version isn't notarized: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// installedApp is where the running app is installed: its bundle.
func installedApp() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, _ = filepath.EvalSymlinks(executable)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if !strings.HasSuffix(bundle, ".app") {
		return "", errors.New("the app isn't running from its bundle")
	}
	return bundle, nil
}
