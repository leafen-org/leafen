package app

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// A .leafenapp file is a zip of leafen-app.json, ui/, workers/, data/ and
// signature.json. Reading it refuses anything that could reach outside its
// own names or swell past its limits; what is read is fingerprinted by its
// files so an installed version is exactly those bytes, and a signature, if
// any, must sign that fingerprint with the key the manifest names. A large
// package is read from a file on disk, each of its files spooled to a file of
// its own, so reading never holds more than the manifest and data/ in memory
// (document apps decision of 2026-10-10, 5).

const (
	appManifestFile       = "leafen-app.json"
	appSignatureFile      = "signature.json"
	appSignatureMessage   = "leafen-app-package-v1\n"
	appTrustBuiltin       = "builtin"
	appTrustCatalog       = "catalog"
	appTrustSigned        = "signed"
	appTrustUnsigned      = "unsigned"
	appPackageTopFolderUI = "ui/"
	// site/ holds the pages a public endpoint serves (app_platform_endpoint_pages.go).
	appEndpointPagesFolder = "site/"
	// data/ files are read whole to check them and to seed an app's data.
	appPackageMaxDataFileBytes = 16 << 20
)

var (
	errAppPackageInvalid   = errors.New(errorCodeAppPackageInvalid)
	errAppPackageTooLarge  = errors.New(errorCodeAppPackageTooLarge)
	errAppSignatureInvalid = errors.New(errorCodeAppPackageSignatureInvalid)
)

type appPackageFile struct {
	Path        string
	Size        int64
	SHA256      string
	ContentType string
	// Data holds the file's bytes, or SpoolPath names the file on disk they
	// were spooled to.
	Data      []byte
	SpoolPath string
}

type appPackage struct {
	Hash           string
	Manifest       appManifest
	ManifestJSON   []byte
	Files          []appPackageFile
	Size           int64
	KeyFingerprint string
	Signed         bool
}

type appPackageError struct {
	cause    error
	problems []string
}

func (failure *appPackageError) Error() string {
	return failure.cause.Error() + ": " + strings.Join(failure.problems, "; ")
}

func (failure *appPackageError) Unwrap() error { return failure.cause }

func appPackageFailure(cause error, problems ...string) error {
	return &appPackageError{cause: cause, problems: problems}
}

// readAppPackage opens a package's bytes, checks every name and size, the
// manifest against its files and the signature against its fingerprint.
func readAppPackage(raw []byte) (appPackage, error) {
	if len(raw) == 0 || int64(len(raw)) > appPackageMaxBytes {
		return appPackage{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("a package is at most %d bytes", int64(appPackageMaxBytes)))
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the file isn't a zip archive")
	}
	return readAppPackageArchive(archive, "")
}

// readAppPackageFile reads a package from a file on disk of at most limit
// bytes, spooling its files into spool, a folder the caller removes.
func readAppPackageFile(packagePath, spool string, limit int64) (appPackage, error) {
	file, err := os.Open(packagePath)
	if err != nil {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the package can't be read")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the package can't be read")
	}
	if info.Size() > limit {
		return appPackage{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("a package is at most %d bytes on this server", limit))
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the file isn't a zip archive")
	}
	return readAppPackageArchive(archive, spool)
}

func readAppPackageArchive(archive *zip.Reader, spool string) (appPackage, error) {
	if len(archive.File) > appPackageMaxFiles+1 {
		return appPackage{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("a package has at most %d files", appPackageMaxFiles))
	}
	files := []appPackageFile{}
	seen := map[string]bool{}
	var total int64
	var signature []byte
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name, problem := appPackagePath(entry)
		if problem != "" {
			return appPackage{}, appPackageFailure(errAppPackageInvalid, problem)
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return appPackage{}, appPackageFailure(errAppPackageInvalid, fmt.Sprintf("%q appears twice", name))
		}
		seen[folded] = true
		if spool != "" && name != appManifestFile && name != appSignatureFile && !strings.HasPrefix(name, "data/") {
			file, err := spoolAppPackageEntry(entry, appPackageMaxUncompressedBytes-total, spool, len(files))
			if err != nil {
				return appPackage{}, err
			}
			total += file.Size
			file.Path, file.ContentType = name, appContentType(name)
			files = append(files, file)
			continue
		}
		remaining := int64(appPackageMaxUncompressedBytes) - total
		if spool != "" && remaining > appPackageMaxDataFileBytes {
			// Read whole even from a large package: the manifest, the
			// signature and data/, all small.
			remaining = appPackageMaxDataFileBytes
		}
		data, err := readAppPackageEntry(entry, remaining)
		if err != nil {
			return appPackage{}, err
		}
		total += int64(len(data))
		if name == appSignatureFile {
			signature = data
			continue
		}
		digest := sha256.Sum256(data)
		files = append(files, appPackageFile{
			Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), ContentType: appContentType(name), Data: data,
		})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })

	pkg := appPackage{Files: files, Size: total, Hash: appPackageHash(files)}
	present := map[string]bool{}
	for _, file := range files {
		present[file.Path] = true
		if file.Path == appManifestFile {
			pkg.ManifestJSON = file.Data
		}
	}
	if pkg.ManifestJSON == nil {
		return appPackage{}, appPackageFailure(errAppPackageInvalid, "the package has no "+appManifestFile)
	}
	if len(pkg.ManifestJSON) > appManifestMaxBytes {
		return appPackage{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%s is at most %d bytes", appManifestFile, appManifestMaxBytes))
	}
	decoder := json.NewDecoder(bytes.NewReader(pkg.ManifestJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pkg.Manifest); err != nil {
		return appPackage{}, appPackageFailure(errors.New(errorCodeAppManifestInvalid), appJSONProblem(appManifestFile, pkg.ManifestJSON, err))
	}
	if problems := appManifestProblems(pkg.Manifest, present); len(problems) > 0 {
		return appPackage{}, appPackageFailure(errors.New(errorCodeAppManifestInvalid), problems...)
	}
	contents := map[string][]byte{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, "data/") && file.Size > appPackageMaxDataFileBytes {
			return appPackage{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%q is larger than a data file may be (%d bytes)", file.Path, appPackageMaxDataFileBytes))
		}
		if file.Data != nil {
			contents[file.Path] = file.Data
		}
	}
	if problems := appDataProblems(pkg.Manifest, contents); len(problems) > 0 {
		return appPackage{}, appPackageFailure(errors.New(errorCodeAppManifestInvalid), problems...)
	}
	if signature != nil {
		fingerprint, err := verifyAppPackageSignature(signature, pkg.Hash)
		if err != nil {
			return appPackage{}, err
		}
		if pkg.Manifest.Developer.KeyFingerprint != fingerprint {
			return appPackage{}, appPackageFailure(errAppSignatureInvalid, "the signing key isn't the one developer.keyFingerprint names")
		}
		pkg.KeyFingerprint, pkg.Signed = fingerprint, true
	}
	return pkg, nil
}

// appPackagePath takes an entry's name only when it is a plain relative
// path in normalized form, of a regular file, under the known folders.
func appPackagePath(entry *zip.File) (string, string) {
	name := entry.Name
	if !entry.Mode().IsRegular() {
		return "", fmt.Sprintf("%q isn't a regular file", name)
	}
	if !utf8.ValidString(name) || norm.NFC.String(name) != name || utf8.RuneCountInString(name) > appPackageMaxPathRunes ||
		strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name ||
		name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Sprintf("%q isn't a plain relative path", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") && part != ".well-known" {
			return "", fmt.Sprintf("%q has a hidden or empty part", name)
		}
	}
	if name == appManifestFile || name == appSignatureFile {
		return name, ""
	}
	// templates/ holds the blank files a file opener starts new documents from.
	for _, folder := range []string{appPackageTopFolderUI, "workers/", "data/", "templates/", appEndpointPagesFolder} {
		if strings.HasPrefix(name, folder) {
			return name, ""
		}
	}
	return "", fmt.Sprintf("%q isn't under ui/, workers/, data/, templates/ or site/", name)
}

// spoolAppPackageEntry copies an entry to a file of its own in spool, at
// most the file limit and what is left of the package's, whatever its
// header claims, and fingerprints it on the way.
func spoolAppPackageEntry(entry *zip.File, remaining int64, spool string, index int) (appPackageFile, error) {
	limit := int64(appPackageMaxFileBytes)
	if remaining < limit {
		limit = remaining
	}
	if entry.UncompressedSize64 > uint64(limit) {
		return appPackageFile{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%q is larger than the package allows", entry.Name))
	}
	reader, err := entry.Open()
	if err != nil {
		return appPackageFile{}, appPackageFailure(errAppPackageInvalid, fmt.Sprintf("%q can't be read", entry.Name))
	}
	defer reader.Close()
	target := filepath.Join(spool, strconv.Itoa(index))
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return appPackageFile{}, err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, digest), io.LimitReader(reader, limit+1))
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return appPackageFile{}, appPackageFailure(errAppPackageInvalid, fmt.Sprintf("%q can't be read", entry.Name))
	}
	if written > limit {
		return appPackageFile{}, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%q is larger than the package allows", entry.Name))
	}
	return appPackageFile{Size: written, SHA256: hex.EncodeToString(digest.Sum(nil)), SpoolPath: target}, nil
}

// readAppPackageEntry reads at most the file limit and what is left of the
// package's, whatever its header claims.
func readAppPackageEntry(entry *zip.File, remaining int64) ([]byte, error) {
	limit := int64(appPackageMaxFileBytes)
	if remaining < limit {
		limit = remaining
	}
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%q is larger than the package allows", entry.Name))
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, appPackageFailure(errAppPackageInvalid, fmt.Sprintf("%q can't be read", entry.Name))
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, appPackageFailure(errAppPackageInvalid, fmt.Sprintf("%q can't be read", entry.Name))
	}
	if int64(len(data)) > limit {
		return nil, appPackageFailure(errAppPackageTooLarge, fmt.Sprintf("%q is larger than the package allows", entry.Name))
	}
	return data, nil
}

// appPackageHash fingerprints the files, signature aside: each path, size
// and SHA-256 on a line, in path order.
func appPackageHash(files []appPackageFile) string {
	digest := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(digest, file.Path+"\x00"+strconv.FormatInt(file.Size, 10)+"\x00"+file.SHA256+"\n")
	}
	return hex.EncodeToString(digest.Sum(nil))
}

type appSignature struct {
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

// verifyAppPackageSignature checks an Ed25519 signature of the package
// fingerprint and returns the SHA-256 fingerprint of the key that made it.
func verifyAppPackageSignature(raw []byte, packageHash string) (string, error) {
	signature := appSignature{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&signature) != nil || signature.Algorithm != "ed25519" {
		return "", appPackageFailure(errAppSignatureInvalid, appSignatureFile+" isn't an ed25519 signature")
	}
	key, keyErr := base64.StdEncoding.DecodeString(signature.PublicKey)
	signed, signedErr := base64.StdEncoding.DecodeString(signature.Signature)
	if keyErr != nil || signedErr != nil || len(key) != ed25519.PublicKeySize || len(signed) != ed25519.SignatureSize {
		return "", appPackageFailure(errAppSignatureInvalid, appSignatureFile+" has a malformed key or signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(key), []byte(appSignatureMessage+packageHash), signed) {
		return "", appPackageFailure(errAppSignatureInvalid, "the signature doesn't match the package's files")
	}
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:]), nil
}

func appContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".wasm":
		return "application/wasm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}
