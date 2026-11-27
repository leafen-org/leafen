package app

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// cloudflared for the administrator's own tunnel: the one on the path, or
// the latest release fetched once, and running it (server_tunnel.go keeps it
// running).

// cloudflaredPath finds cloudflared, fetching it the first time.
func cloudflaredPath(ctx context.Context, app core.App) (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("LEAFEN_CLOUDFLARED")); explicit != "" {
		return explicit, nil
	}
	if found, err := exec.LookPath("cloudflared"); err == nil {
		return found, nil
	}
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	local := filepath.Join(app.DataDir(), "bin", name)
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		return local, nil
	}
	return local, downloadCloudflared(ctx, local)
}

// downloadCloudflared fetches the latest official release for this computer,
// checks it against the SHA-256 the release publishes, and puts it at target.
func downloadCloudflared(ctx context.Context, target string) error {
	asset := cloudflaredAsset()
	if asset == "" {
		return fmt.Errorf("no cloudflared release for %s/%s; put cloudflared on the PATH", runtime.GOOS, runtime.GOARCH)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/cloudflare/cloudflared/releases/latest", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	release := struct {
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}{}
	err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&release)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		return fmt.Errorf("the release list couldn't be read (%d)", response.StatusCode)
	}
	wantURL, wantDigest := "", ""
	for _, candidate := range release.Assets {
		if candidate.Name == asset {
			wantURL, wantDigest = candidate.URL, strings.TrimPrefix(candidate.Digest, "sha256:")
		}
	}
	if parsed, err := url.Parse(wantURL); err != nil || parsed.Scheme != "https" || len(wantDigest) != 64 {
		return errors.New("the release has no checked file for this computer")
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, wantURL, nil)
	if err != nil {
		return err
	}
	response, err = client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the release file couldn't be fetched (%d)", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, serverTunnelBinaryMaxBytes+1))
	if err != nil || len(body) > serverTunnelBinaryMaxBytes {
		return errors.New("the release file is too large")
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != strings.ToLower(wantDigest) {
		return errors.New("the release file doesn't match its published SHA-256")
	}
	if strings.HasSuffix(asset, ".tgz") {
		if body, err = cloudflaredFromArchive(body); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary := target + ".part"
	if err := os.WriteFile(temporary, body, 0o755); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func cloudflaredFromArchive(archive []byte) ([]byte, error) {
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if err != nil {
			return nil, errors.New("the release archive holds no cloudflared")
		}
		if filepath.Base(header.Name) == "cloudflared" && header.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(reader, serverTunnelBinaryMaxBytes))
		}
	}
}

// runOnce runs cloudflared until it exits, saying why it did.
func (tunnel *serverTunnel) runOnce(ctx context.Context, settings serverTunnelSettings, token string) tunnelFailure {
	if settings.Mode == serverTunnelTailscale {
		return tunnel.runTailscale(ctx)
	}
	if settings.Mode == serverTunnelOwnAddress {
		return tunnel.runOwnAddressTLS(ctx, settings)
	}
	binary, err := cloudflaredPath(ctx, tunnel.app)
	if err != nil {
		return failedTunnel(tunnelFailureProgram, "cloudflared couldn't be found or fetched: "+err.Error())
	}
	args := []string{"tunnel", "--no-autoupdate"}
	if settings.Mode == serverTunnelQuick {
		// A quick tunnel comes in at the door, where the address it hands
		// on is believed (server_tunnel_door.go).
		door, err := tunnelDoorOf(tunnel.app, tunnel.localURL)
		if err != nil {
			return failedTunnel(tunnelFailureDoor, "The tunnel's door couldn't be opened: "+err.Error())
		}
		args = append(args, "--url", door)
	} else {
		args = append(args, "run")
	}
	command := exec.CommandContext(ctx, binary, args...)
	// The token goes in the environment, never on the command line others see.
	command.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
	output, err := command.StderrPipe()
	if err != nil {
		return failedTunnel(tunnelFailureProgram, err.Error())
	}
	command.Stdout = command.Stderr
	if err := command.Start(); err != nil {
		return failedTunnel(tunnelFailureProgram, "cloudflared didn't start: "+err.Error())
	}
	last := ""
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 64<<10)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, " ERR ") || strings.Contains(line, "error=") {
			last = strings.TrimSpace(line)
		}
		switch settings.Mode {
		case serverTunnelQuick:
			if found := quickTunnelAddress.FindString(line); found != "" && runningTunnelOrigin() == "" {
				tunnel.set(serverTunnelStateOn, found, noTunnelFailure)
				serverBackgroundOf(tunnel.app).behind("quick tunnel address", func() { noteQuickTunnelAddress(tunnel.app, found) })
			}
		case serverTunnelToken:
			if strings.Contains(line, "Registered tunnel connection") && runningTunnelOrigin() == "" {
				tunnel.set(serverTunnelStateOn, settings.Address, noTunnelFailure)
			}
		}
	}
	err = command.Wait()
	if last == "" && err != nil {
		last = "cloudflared stopped: " + err.Error()
	}
	if last == "" {
		last = "cloudflared stopped."
	}
	return failedTunnel(tunnelFailureStopped, last)
}

// cloudflaredAsset is the official release file for this computer.
func cloudflaredAsset() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "linux/arm", "linux/386":
		return "cloudflared-linux-" + runtime.GOARCH
	case "darwin/amd64", "darwin/arm64":
		return "cloudflared-darwin-" + runtime.GOARCH + ".tgz"
	case "windows/amd64", "windows/386":
		return "cloudflared-windows-" + runtime.GOARCH + ".exe"
	}
	return ""
}
