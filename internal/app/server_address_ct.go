package app

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The server keeps whoever answers for its address's name honest (6-2 of the
// decision of 2026-10-17, watching the server's own address since
// 2026-10-19): Tailscale answers the ts.net zone's DNS and relays Funnel, so
// it — or someone who broke into it — could get a certificate for the
// server's name and open the connections it relays. Every certificate is
// published in Certificate Transparency logs, so the server looks there for
// its name about every six hours (server_address_ct_watch.go). One it didn't
// get itself, still valid and issued since it took the name — or any
// certificate for the whole zone — makes it tell its administrators and close
// the public address until an administrator says the certificate is theirs.

const (
	// addressCTSourceEnv points at a crt.sh-shaped search of the logs, for a
	// stand-in in development.
	addressCTSourceEnv     = "LEAFEN_CT_SOURCE"
	addressCTDefaultSource = "https://crt.sh"
	addressCTEvery         = 6 * time.Hour
	addressIssuedFile      = "issued.json"
	addressCTAlertFile     = "certificate-alert.json"
	// addressWatchFolder holds the notes for the addresses watched, a
	// folder per name.
	addressWatchFolder = "address-watch"

	serverTunnelCertificateAlertPath = serverTunnelPath + "/certificate-alert"
)

// addressIssued is what the server knows of its own certificates: when it took
// its name and the serial numbers of the certificates it got (or its
// administrators said were theirs).
type addressIssued struct {
	Since   int64    `json:"since"`
	Serials []string `json:"serials"`
}

// addressCertificateAlert is a certificate for the address the server didn't
// get.
type addressCertificateAlert struct {
	At        int64  `json:"at"`
	Name      string `json:"name"`
	Issuer    string `json:"issuer"`
	Serial    string `json:"serial"`
	NotBefore string `json:"notBefore"`
	Entry     int64  `json:"entry"`
}

func normalSerial(serial string) string {
	value, ok := new(big.Int).SetString(strings.TrimSpace(strings.ReplaceAll(serial, ":", "")), 16)
	if !ok {
		return strings.ToLower(serial)
	}
	return value.Text(16)
}

func readAddressIssued(dir string) addressIssued {
	issued := addressIssued{}
	if data, err := os.ReadFile(filepath.Join(dir, addressIssuedFile)); err == nil {
		_ = json.Unmarshal(data, &issued)
	}
	return issued
}

func writeAddressIssued(dir string, issued addressIssued) error {
	data, err := json.Marshal(issued)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, addressIssuedFile), data, 0o600)
}

// noteAddressName keeps when the server took its name, the first time.
func noteAddressName(dir string) {
	issued := readAddressIssued(dir)
	if issued.Since == 0 {
		issued.Since = time.Now().UnixMilli()
		_ = writeAddressIssued(dir, issued)
	}
}

// noteAddressSerial keeps a serial number as the server's own.
func noteAddressSerial(dir, serial string) error {
	issued := readAddressIssued(dir)
	if issued.Since == 0 {
		issued.Since = time.Now().UnixMilli()
	}
	if serial = normalSerial(serial); !slices.Contains(issued.Serials, serial) {
		issued.Serials = append(issued.Serials, serial)
	}
	return writeAddressIssued(dir, issued)
}

// noteAddressCertificate keeps a certificate the server got as its own.
func noteAddressCertificate(dir string, der []byte) error {
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	return noteAddressSerial(dir, certificate.SerialNumber.Text(16))
}

func readAddressCertificateAlert(dir string) *addressCertificateAlert {
	data, err := os.ReadFile(filepath.Join(dir, addressCTAlertFile))
	if err != nil {
		return nil
	}
	alert := &addressCertificateAlert{}
	if json.Unmarshal(data, alert) != nil {
		return nil
	}
	return alert
}

// ctEntry is one certificate as crt.sh lists it.
type ctEntry struct {
	ID         int64  `json:"id"`
	IssuerName string `json:"issuer_name"`
	NameValue  string `json:"name_value"`
	NotBefore  string `json:"not_before"`
	NotAfter   string `json:"not_after"`
	Serial     string `json:"serial_number"`
}

func parseCTTime(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// findForeignAddressCertificate looks in one search of the logs for a
// certificate for the address, or for the whole zone, that the server didn't
// get.
func findForeignAddressCertificate(ctx context.Context, dir, address string, from addressCTSource) (*addressCertificateAlert, error) {
	issued := readAddressIssued(dir)
	client := &http.Client{Timeout: addressCTRequestTimeout}
	if from.Kind == "certspotter" {
		return findForeignInCertSpotter(ctx, client, from.Base, address, issued)
	}
	source := from.Base
	_, zone, _ := strings.Cut(address, ".")
	for _, query := range []string{address, "*." + zone} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source+"/?q="+url.QueryEscape(query)+"&output=json", nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		data, err := ctAnswer(response, source)
		if err != nil {
			return nil, err
		}
		entries := []ctEntry{}
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, err
		}
		for _, entry := range entries {
			names := strings.Split(strings.ToLower(entry.NameValue), "\n")
			if !slices.Contains(names, query) {
				continue
			}
			notBefore, okBefore := parseCTTime(entry.NotBefore)
			notAfter, okAfter := parseCTTime(entry.NotAfter)
			if !okBefore || !okAfter || time.Now().After(notAfter) ||
				(query == address && notBefore.UnixMilli() < issued.Since-24*time.Hour.Milliseconds()) {
				continue
			}
			if slices.Contains(issued.Serials, normalSerial(entry.Serial)) {
				continue
			}
			return &addressCertificateAlert{At: time.Now().UnixMilli(), Name: query, Issuer: entry.IssuerName,
				Serial: normalSerial(entry.Serial), NotBefore: entry.NotBefore, Entry: entry.ID}, nil
		}
	}
	return nil, nil
}

// raiseAddressCertificateAlert keeps the alert and tells the administrators in
// their inbox.
func raiseAddressCertificateAlert(app core.App, dir, address string, alert *addressCertificateAlert) {
	data, _ := json.Marshal(alert)
	if err := os.WriteFile(filepath.Join(dir, addressCTAlertFile), data, 0o600); err != nil {
		app.Logger().Warn("leafen certificate alert not kept", "error", err)
	}
	app.Logger().Warn("a certificate for this server's address came from elsewhere; the public address is off",
		"address", address, "issuer", alert.Issuer, "serial", alert.Serial)
	administrators, err := serverAdministrators(app)
	if err != nil {
		app.Logger().Warn("leafen certificate alert notice failed", "error", err)
		return
	}
	now := time.Now().UnixMilli()
	key := "address-certificate:" + appNoticeID("address-certificate", alert.Serial)
	projections := []productNotificationProjection{}
	for _, administrator := range administrators {
		workspaceID, err := personNoticeWorkspace(app, administrator, "")
		if err != nil || workspaceID == "" {
			continue
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("address-certificate", alert.Serial, administrator), EventID: appNoticeID("address-certificate-event", alert.Serial),
			WorkspaceID: workspaceID, UserID: administrator, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "address-certificate", Title: address,
			Target: "/admin/settings?section=tunnel", OccurredAt: now,
		})
	}
	if len(projections) > 0 {
		if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
			app.Logger().Warn("leafen certificate alert notice failed", "error", err)
		}
	}
}

// clearAddressCertificateAlertRoute is an administrator saying the
// certificate is theirs: it counts as the server's own and the public address
// comes back.
func clearAddressCertificateAlertRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	settings, err := loadServerTunnelSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't read the tunnel.", err)
	}
	dir := addressNotesDir(event.App, settings)
	if alert := readAddressCertificateAlert(dir); dir != "" && alert != nil {
		if err := noteAddressSerial(dir, alert.Serial); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't keep the certificate as this server's.", err)
		}
		if err := os.Remove(filepath.Join(dir, addressCTAlertFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't clear the alert.", err)
		}
	}
	tunnel := serverTunnelOf(event.App)
	if settings.Mode == serverTunnelTailscale {
		_ = tunnel.apply(settings)
	}
	return event.JSON(http.StatusOK, tunnel.status(settings))
}

// addressNotesDir is where the server keeps what it knows of the
// certificates for the Tailscale address it serves: the ones it got, one it
// didn't, and when it last read the logs — a folder per name, so an earlier
// name's notes don't count for it; "" for an address whose certificate the
// administrator holds.
func addressNotesDir(app core.App, settings serverTunnelSettings) string {
	if settings.Mode == serverTunnelTailscale && settings.TailscaleAddress != "" {
		return filepath.Join(app.DataDir(), addressWatchFolder, settings.TailscaleAddress)
	}
	return ""
}

// certificateAlert is the alert the server keeps, if any.
func (tunnel *serverTunnel) certificateAlert(settings serverTunnelSettings) *addressCertificateAlert {
	if tunnel.app == nil || addressNotesDir(tunnel.app, settings) == "" {
		return nil
	}
	return readAddressCertificateAlert(addressNotesDir(tunnel.app, settings))
}

// certificateWatch is when the logs were last read and last couldn't be, if
// ever.
func (tunnel *serverTunnel) certificateWatch(settings serverTunnelSettings) *addressCTWatchStatus {
	if tunnel.app == nil || addressNotesDir(tunnel.app, settings) == "" {
		return nil
	}
	watch := readAddressCTWatch(addressNotesDir(tunnel.app, settings))
	if watch.CheckedAt == 0 && watch.FailedAt == 0 {
		return nil
	}
	return &addressCTWatchStatus{CheckedAt: watch.CheckedAt, FailedAt: watch.FailedAt}
}
