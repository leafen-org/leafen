//go:build (darwin && !ios) || windows

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The programs the app fetches for the person — screen control's driver,
// the local model engine — come as another project's release: the archive
// named here, checked against the hash named here, unpacked in a folder of
// the app's own with nothing leaving it.

// fetchRelease fetches the archive at url into parent, checks it is at most
// most bytes and has the SHA-256 sum, and unpacks it in a new folder there,
// which it answers; links stay inside that folder when allowed, else the
// archive may hold only files and folders. progress, when given, hears the
// bytes fetched.
func fetchRelease(ctx context.Context, url, sum string, most int64, parent string, links bool, progress func(done, total int64)) (string, error) {
	archive, err := os.CreateTemp(parent, "release-*.download")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New(response.Status)
	}
	digest := sha256.New()
	var into io.Writer = io.MultiWriter(archive, digest)
	if progress != nil {
		into = io.MultiWriter(into, &progressWriter{total: response.ContentLength, heard: progress})
	}
	written, err := io.Copy(into, io.LimitReader(response.Body, most+1))
	if err != nil {
		return "", err
	}
	if written > most || hex.EncodeToString(digest.Sum(nil)) != sum {
		return "", errors.New("the archive fetched isn't the release Leafen names")
	}
	unpacked, err := os.MkdirTemp(parent, "release-*.unpack")
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(url, ".zip") {
		err = unpackReleaseZip(archive.Name(), unpacked, most*4)
	} else {
		err = unpackReleaseTar(archive.Name(), unpacked, most*4, links)
	}
	if err != nil {
		os.RemoveAll(unpacked)
		return "", err
	}
	return unpacked, nil
}

// progressWriter tells how much of a download has come.
type progressWriter struct {
	done, total int64
	heard       func(done, total int64)
}

func (writer *progressWriter) Write(data []byte) (int, error) {
	writer.done += int64(len(data))
	writer.heard(writer.done, writer.total)
	return len(data), nil
}

// releaseEntryPath is where an archive's entry goes, refused when it would
// leave the folder.
func releaseEntryPath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive names %q outside its folder", name)
	}
	return filepath.Join(root, clean), nil
}

func unpackReleaseTar(path, root string, most int64, links bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	reader := tar.NewReader(gz)
	total := int64(0)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := releaseEntryPath(root, header.Name)
		if err != nil {
			return err
		}
		switch {
		case header.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case header.Typeflag == tar.TypeReg:
			if total += header.Size; total > most {
				return errors.New("the archive is too large")
			}
			if err := writeReleaseFile(target, reader, os.FileMode(header.Mode)&0o755); err != nil {
				return err
			}
		case header.Typeflag == tar.TypeSymlink && links && header.Linkname == filepath.Base(header.Linkname) &&
			header.Linkname != ".." && header.Linkname != ".":
			// A library's other names, beside it in the same folder.
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		default:
			// Devices, and links elsewhere, have no place in a release.
			return fmt.Errorf("the archive holds %q, which isn't a file or folder", header.Name)
		}
	}
}

func unpackReleaseZip(path, root string, most int64) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	total := uint64(0)
	for _, entry := range reader.File {
		target, err := releaseEntryPath(root, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("the archive holds %q, which isn't a file or folder", entry.Name)
		}
		if total += entry.UncompressedSize64; total > uint64(most) {
			return errors.New("the archive is too large")
		}
		opened, err := entry.Open()
		if err != nil {
			return err
		}
		err = writeReleaseFile(target, opened, 0o755)
		opened.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeReleaseFile(target string, from io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, from); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
