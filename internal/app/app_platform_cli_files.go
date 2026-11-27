//go:build !leafen_noapps

package app

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The CLI works on a package as a file: it packs a folder into a file a
// file at a time, checks a package from disk spooling its files to a
// throwaway folder, and signs from one file into another, so a package near
// the size limit never sits in memory whole.

// readAppPackagePath checks a package on disk as Leafen would.
func readAppPackagePath(path string) (appPackage, error) {
	spool, err := os.MkdirTemp("", "leafen-app-check-*")
	if err != nil {
		return appPackage{}, err
	}
	defer os.RemoveAll(spool)
	return readAppPackageFile(path, spool, appPackageMaxBytes)
}

// appTemporaryFile is a new file in dir that a rename can put in place.
func appTemporaryFile(dir string) (*os.File, error) {
	if dir == "" {
		dir = "."
	}
	return os.CreateTemp(dir, ".leafen-app-*")
}

// packAppFolderFile packs a folder into a new file in dir.
func packAppFolderFile(folder, dir string) (string, error) {
	file, err := appTemporaryFile(dir)
	if err != nil {
		return "", err
	}
	if err := writeAppFolderPackage(file, os.DirFS(folder), "."); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// putAppPackageFile moves a finished package file to path.
func putAppPackageFile(temporary, path string) error {
	if err := os.Chmod(temporary, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// copyAppPackageEntries writes a package's files but its signature to a new
// file in dir, then signature when given.
func copyAppPackageEntries(archive *zip.Reader, dir string, signature []byte) (string, error) {
	file, err := appTemporaryFile(dir)
	if err != nil {
		return "", err
	}
	writer := zip.NewWriter(file)
	for _, entry := range archive.File {
		if entry.Name == appSignatureFile {
			continue
		}
		if err = writer.Copy(entry); err != nil {
			break
		}
	}
	if err == nil && signature != nil {
		var created interface{ Write([]byte) (int, error) }
		if created, err = writer.Create(appSignatureFile); err == nil {
			_, err = created.Write(signature)
		}
	}
	if err == nil {
		err = writer.Close()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// signAppPackageFile writes source signed with the key to output, the two
// may be one file.
func signAppPackageFile(source, output string, private ed25519.PrivateKey, fingerprint string) (appPackage, error) {
	input, err := os.Open(source)
	if err != nil {
		return appPackage{}, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return appPackage{}, err
	}
	archive, err := zip.NewReader(input, info.Size())
	if err != nil {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the file isn't a zip archive")
	}
	dir := filepath.Dir(output)
	unsigned, err := copyAppPackageEntries(archive, dir, nil)
	if err != nil {
		return appPackage{}, err
	}
	defer os.Remove(unsigned)
	pkg, err := readAppPackagePath(unsigned)
	if err != nil {
		return appPackage{}, err
	}
	if pkg.Manifest.Developer.KeyFingerprint != fingerprint {
		return appPackage{}, fmt.Errorf("the manifest's developer.keyFingerprint must be this key's %s; set it and build again", fingerprint)
	}
	signature, _ := json.Marshal(appSignature{Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(appSignatureMessage+pkg.Hash)))})
	signed, err := copyAppPackageEntries(archive, dir, signature)
	if err != nil {
		return appPackage{}, err
	}
	pkg, err = readAppPackagePath(signed)
	if err == nil {
		err = putAppPackageFile(signed, output)
	}
	if err != nil {
		_ = os.Remove(signed)
		return appPackage{}, err
	}
	return pkg, nil
}

// appPackageFileDigest is a package file's SHA-256 and size.
func appPackageFileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

// copyAppPackageFile copies a package file to path.
func copyAppPackageFile(source, path string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	file, err := appTemporaryFile(filepath.Dir(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(file, input)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = putAppPackageFile(file.Name(), path)
	}
	if err != nil {
		_ = os.Remove(file.Name())
	}
	return err
}

// appPublishIcon is the package's icon as the data address its catalog entry
// carries, or "" and why it is left out — the lists then draw the app's
// first letter. path is a package already read whole.
func appPublishIcon(path, icon string) (string, string) {
	if icon == "" {
		return "", ""
	}
	if appIconKind(icon) == "" {
		return "", icon + " isn't a PNG, JPEG, WebP or SVG picture"
	}
	tooLarge := fmt.Sprintf("%s is over %d KiB, the most a catalog entry carries", icon, appCatalogIconMaxBytes>>10)
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err.Error()
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if name, problem := appPackagePath(entry); problem != "" || name != icon {
			continue
		}
		if entry.UncompressedSize64 > appCatalogIconMaxBytes {
			return "", tooLarge
		}
		reader, err := entry.Open()
		if err != nil {
			return "", err.Error()
		}
		data, err := io.ReadAll(io.LimitReader(reader, appCatalogIconMaxBytes+1))
		reader.Close()
		if err != nil {
			return "", err.Error()
		}
		if len(data) > appCatalogIconMaxBytes {
			return "", tooLarge
		}
		if data := appIconDataURL(icon, data, appCatalogIconMaxBytes); data != "" {
			return data, ""
		}
		return "", icon + " is empty"
	}
	return "", icon + " isn't in the package"
}
