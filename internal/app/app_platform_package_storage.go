package app

import (
	"database/sql"
	"errors"
	"io"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// An app's files are kept in the server's file storage by their SHA-256, as
// people's files are, so a package of up to two gigabytes never sits in the
// database or whole in memory (document apps decision of 2026-10-10, 5); its
// file rows keep the path, size, fingerprint and type. Packages kept before
// hold their bytes in the row, which is read as before. The administrator may
// set a lower limit on a package's size than the most Leafen takes.

const appPackageFileStorePrefix = "leafen_app_files/"

// appPackageLimit is the largest package this server takes now.
func appPackageLimit(app core.App) int64 {
	settings, err := loadAppPlatformSettings(app)
	if err != nil || settings.MaxPackageBytes <= 0 || settings.MaxPackageBytes > appPackageMaxBytes {
		return appPackageMaxBytes
	}
	return settings.MaxPackageBytes
}

func appPackageFileKey(sha string) string {
	return appPackageFileStorePrefix + sha
}

// keepAppPackageFiles puts a package's files into the file storage, those
// not kept already, from memory or from where they were spooled.
func keepAppPackageFiles(app core.App, files []appPackageFile) error {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	for _, file := range files {
		key := appPackageFileKey(file.SHA256)
		if exists, err := fsys.Exists(key); err != nil {
			return err
		} else if exists {
			continue
		}
		if file.SpoolPath != "" {
			spooled, err := filesystem.NewFileFromPath(file.SpoolPath)
			if err != nil {
				return err
			}
			if err := fsys.UploadFile(spooled, key); err != nil {
				return err
			}
			continue
		}
		if err := fsys.Upload(file.Data, key); err != nil {
			return err
		}
	}
	return nil
}

// storedAppPackageFile is a kept file's row: its bytes when the row holds
// them, or where the file storage keeps them.
type storedAppPackageFile struct {
	ContentType string `db:"content_type"`
	Size        int64  `db:"size"`
	SHA256      string `db:"sha256"`
	Data        []byte `db:"data"`
}

func findAppPackageFile(app core.App, packageHash, path string) (storedAppPackageFile, error) {
	file := storedAppPackageFile{}
	err := app.DB().NewQuery(`SELECT [[content_type]], [[size]], [[sha256]], [[data]] FROM {{leafen_app_package_files}}
		WHERE [[package_hash]] = {:hash} AND [[path]] = {:path}`).Bind(dbx.Params{"hash": packageHash, "path": path}).One(&file)
	return file, err
}

// inRow reports whether the row itself holds the file's bytes.
func (file storedAppPackageFile) inRow() bool {
	return int64(len(file.Data)) == file.Size
}

// loadAppPackageFile reads a kept file whole, for the files Leafen itself
// reads: data/, workers' modules, the icon.
func loadAppPackageFile(app core.App, packageHash, path string) ([]byte, error) {
	file, err := findAppPackageFile(app, packageHash, path)
	if err != nil || file.inRow() {
		return file.Data, err
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()
	reader, err := fsys.GetReader(appPackageFileKey(file.SHA256))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, file.Size+1))
	if err == nil && int64(len(data)) != file.Size {
		err = errors.New("the app file in storage isn't the size kept for it")
	}
	return data, err
}

// openAppPackageFile opens a kept file from the file storage to serve it;
// the caller closes both. It answers sql.ErrNoRows for a file the row holds.
func openAppPackageFile(app core.App, file storedAppPackageFile) (*filesystem.System, io.ReadSeekCloser, error) {
	if file.inRow() {
		return nil, nil, sql.ErrNoRows
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, nil, err
	}
	reader, err := fsys.GetReader(appPackageFileKey(file.SHA256))
	if err != nil {
		_ = fsys.Close()
		return nil, nil, err
	}
	return fsys, reader, nil
}
