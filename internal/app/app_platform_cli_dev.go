//go:build !leafen_noapps

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cobra"
)

// `leafen app dev` tries an app on a throwaway Leafen: a server of this
// executable on this machine's loopback, in a new temporary folder with a
// made-up administrator and workspace, so nothing touches real data. It
// installs the app for that workspace's organization and installs it again,
// as version <version>-dev.<n>, whenever the folder's files change.

func appDevCommand() *cobra.Command {
	port, keep := 0, false
	command := &cobra.Command{
		Use: "dev [folder]", Short: "Try an app on a throwaway local Leafen, reinstalling it when its files change", Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runAppDev(ctx, command.OutOrStdout(), appFolderArgument(args), port, keep)
		},
	}
	command.Flags().IntVar(&port, "port", 0, "the local port to serve on (default: a free one)")
	command.Flags().BoolVar(&keep, "keep", false, "keep the throwaway server's data folder when done")
	return command
}

type appDevServer struct {
	base, token, tenancy, installID string
	client                          *http.Client
}

func runAppDev(ctx context.Context, out io.Writer, folder string, port int, keep bool) error {
	if _, err := os.Stat(filepath.Join(folder, appManifestFile)); err != nil {
		return fmt.Errorf("%s has no %s: run `leafen app init` first", folder, appManifestFile)
	}
	if port == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		port = listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
	}
	data, err := os.MkdirTemp("", "leafen-app-dev-")
	if err != nil {
		return err
	}
	if !keep {
		defer os.RemoveAll(data)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.Create(filepath.Join(data, "server.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	server := exec.Command(executable, "--dir", filepath.Join(data, "data"), "serve", "--http", address)
	server.Stdout, server.Stderr = logFile, logFile
	// The throwaway server opens its first setup to this command's own code.
	setupCode := appDevPassword()
	server.Env = append(os.Environ(), "LEAFEN_PUBLIC_URL=http://"+address, "LEAFEN_SETUP_OPEN_MODE=print", setupCodeEnv+"="+setupCode)
	if err := server.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	defer func() {
		_ = server.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = server.Process.Kill()
			<-exited
		}
	}()

	dev := &appDevServer{base: "http://" + address + apiRoot, client: &http.Client{Timeout: 60 * time.Second}}
	email, password := "developer@example.test", appDevPassword()
	if err := dev.start(ctx, exited, email, password, setupCode); err != nil {
		return fmt.Errorf("%w (the server's log is %s)", err, logFile.Name())
	}
	fmt.Fprintf(out, "A throwaway Leafen is at http://%s — log in as %s with %s\n", address, email, password)
	fmt.Fprintf(out, "Its data folder is %s%s. Press Ctrl-C to stop.\n\n", data, map[bool]string{true: " (kept)", false: " (removed when done)"}[keep])

	seen, round := "", 0
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if snapshot := appDevSnapshot(folder); snapshot != seen {
			seen = snapshot
			round++
			if slug, err := dev.install(out, folder, round); err != nil {
				fmt.Fprintf(out, "not installed: %v\n\n", err)
			} else {
				fmt.Fprintf(out, "installed as version %s — open http://%s/apps/%s\n\n", appDevVersionOf(folder, round), address, slug)
			}
			seen = appDevSnapshot(folder)
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-exited:
			return fmt.Errorf("the throwaway server stopped: %v (its log is %s)", err, logFile.Name())
		case <-ticker.C:
		}
	}
}

func appDevPassword() string {
	random := make([]byte, 15)
	_, _ = rand.Read(random)
	return base64.RawURLEncoding.EncodeToString(random)
}

// start waits for the server, claims it with a made-up administrator and
// gives it a workspace whose organization the app is installed for.
func (dev *appDevServer) start(ctx context.Context, exited chan error, email, password, setupCode string) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		response, err := dev.client.Get(dev.base + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			exited <- err
			return fmt.Errorf("the throwaway server didn't start: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return errors.New("the throwaway server didn't answer in 90 seconds")
		}
	}
	session := struct {
		Token string `json:"token"`
	}{}
	if err := dev.call(http.MethodPost, "/setup/claim", map[string]string{"email": email, "name": "Developer", "password": password,
		"passwordConfirm": password, "setupCode": setupCode}, &session); err != nil {
		return err
	}
	dev.token = session.Token
	if err := dev.skipFirstRun(); err != nil {
		return err
	}
	if err := dev.call(http.MethodPost, "/workspaces", map[string]any{"requestId": security.RandomString(20), "name": "App development",
		"skipDefaultPages": true}, nil); err != nil {
		return err
	}
	list := struct {
		Tenancies []struct {
			Key string `json:"key"`
		} `json:"tenancies"`
	}{}
	if err := dev.call(http.MethodGet, "/admin/apps", nil, &list); err != nil {
		return err
	}
	if len(list.Tenancies) == 0 {
		return errors.New("the throwaway server has no organization to install the app for")
	}
	dev.tenancy = list.Tenancies[0].Key
	return nil
}

// skipFirstRun takes the developer straight to the app on sign-in: the
// account's language step ends on the browser's language (2026-11-05), and
// the server's first-run steps for whoever set it up — the outside address
// and the outside agents, whatever steps it has — end at once as their
// window's last step ends them (setup_routes.go), so a step added to the
// first run later never shows here either.
func (dev *appDevServer) skipFirstRun() error {
	if err := dev.call(http.MethodPut, "/account/language", map[string]string{"languagePreference": "system"}, nil); err != nil {
		return err
	}
	return dev.call(http.MethodPost, "/first-run/finish", nil, nil)
}

// install packs the folder as its round's dev version, uploads, installs
// it (accepting what it asks for) and, the first time, lets everyone in the
// organization use it.
func (dev *appDevServer) install(out io.Writer, folder string, round int) (string, error) {
	pkg, folderPackage, err := readAppFolder(out, folder, os.TempDir())
	if err != nil {
		return "", err
	}
	packagePath, err := appDevRepack(folderPackage, appDevVersion(pkg.Manifest.Version, round))
	_ = os.Remove(folderPackage)
	if err != nil {
		return "", err
	}
	defer os.Remove(packagePath)
	packed, err := os.Open(packagePath)
	if err != nil {
		return "", err
	}
	defer packed.Close()
	review := struct {
		PackageHash     string `json:"packageHash"`
		PermissionsHash string `json:"permissionsHash"`
	}{}
	if err := dev.call(http.MethodPost, "/admin/apps/packages", packed, &review); err != nil {
		return "", err
	}
	installed := struct {
		InstallID string `json:"installId"`
	}{}
	if err := dev.call(http.MethodPost, "/admin/apps/installs", map[string]string{"packageHash": review.PackageHash,
		"acceptPermissionsHash": review.PermissionsHash}, &installed); err != nil {
		return "", err
	}
	if installed.InstallID != dev.installID {
		dev.installID = installed.InstallID
		grants := map[string]any{"grants": []any{map[string]any{"tenancyKey": dev.tenancy, "everyone": true, "userIds": []string{}}}}
		if err := dev.call(http.MethodPut, "/admin/apps/installs/"+dev.installID+"/grants", grants, nil); err != nil {
			return "", err
		}
	}
	return pkg.Manifest.Slug, nil
}

func (dev *appDevServer) call(method, path string, body any, answer any) error {
	var reader io.Reader
	contentType := ""
	switch value := body.(type) {
	case nil:
	case *os.File:
		reader, contentType = value, "application/zip"
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		reader, contentType = bytes.NewReader(encoded), "application/json"
	}
	request, err := http.NewRequest(method, dev.base+path, reader)
	if err != nil {
		return err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if file, ok := body.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		request.ContentLength = info.Size()
	}
	if dev.token != "" {
		request.Header.Set("Authorization", "Bearer "+dev.token)
	}
	response, err := dev.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 300 {
		failure := struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}{}
		_ = json.Unmarshal(raw, &failure)
		return fmt.Errorf("%s %s: %d %s %s", method, path, response.StatusCode, failure.Error.Code, failure.Error.Message)
	}
	if answer != nil {
		return json.Unmarshal(raw, answer)
	}
	return nil
}

// appDevRepack writes a packed folder with the version set in its manifest
// to a new file, a file at a time.
func appDevRepack(packed, version string) (string, error) {
	input, err := os.Open(packed)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	archive, err := zip.NewReader(input, info.Size())
	if err != nil {
		return "", err
	}
	file, err := appTemporaryFile(os.TempDir())
	if err != nil {
		return "", err
	}
	if err = appDevRepackTo(file, archive, version); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func appDevRepackTo(w io.Writer, archive *zip.Reader, version string) error {
	writer := zip.NewWriter(w)
	for _, entry := range archive.File {
		if entry.Name != appManifestFile {
			if err := writer.Copy(entry); err != nil {
				return err
			}
			continue
		}
		opened, err := entry.Open()
		if err != nil {
			return err
		}
		manifest := map[string]any{}
		err = json.NewDecoder(opened).Decode(&manifest)
		_ = opened.Close()
		if err != nil {
			return err
		}
		manifest["version"] = version
		encoded, _ := json.MarshalIndent(manifest, "", "  ")
		file, err := writer.Create(appManifestFile)
		if err == nil {
			_, err = file.Write(encoded)
		}
		if err != nil {
			return err
		}
	}
	return writer.Close()
}

func appDevVersionOf(folder string, round int) string {
	raw, _ := os.ReadFile(filepath.Join(folder, appManifestFile))
	manifest := struct {
		Version string `json:"version"`
	}{}
	_ = json.Unmarshal(raw, &manifest)
	return appDevVersion(manifest.Version, round)
}

// appDevSnapshot sums up the folder's package files and worker source, so a
// change to any of them installs again; a worker built from worker/ isn't
// counted, as building it is part of installing.
func appDevSnapshot(folder string) string {
	_, err := os.Stat(filepath.Join(folder, "worker", "go.mod"))
	goWorker, lines := err == nil, []string{}
	_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relative, _ := filepath.Rel(folder, path)
		relative = filepath.ToSlash(relative)
		top, _, _ := strings.Cut(relative, "/")
		if relative != "." && (strings.HasPrefix(entry.Name(), ".") || !appPackageTops[top] && top != "worker") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || goWorker && relative == "workers/main.wasm" {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			lines = append(lines, fmt.Sprintf("%s %d %d", relative, info.Size(), info.ModTime().UnixNano()))
		}
		return nil
	})
	return strings.Join(lines, "\n")
}
