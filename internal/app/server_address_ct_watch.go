package app

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Each server asks the certificate logs on a schedule of its own (2026-10-18:
// every server asked crt.sh as its tunnel came up and then every six hours on
// the dot, and threw a failure away): its first look comes some minutes after
// the tunnel does, later ones every six hours give or take a quarter, and a
// failed look is tried again sooner and then less often, never before the
// source asks. A look that fails is not an alert: the server keeps it as not
// checked, and its administrators see when the logs were last read. The
// logs can be asked of more than one search, each turn starting with the
// next and passing to another when one is slow or refuses.

const (
	// addressCTSourcesEnv lists the searches, "crtsh:" or "certspotter:" and an
	// address, by commas; LEAFEN_CT_SOURCE is one crt.sh-shaped search.
	addressCTSourcesEnv = "LEAFEN_CT_SOURCES"
	addressCTWatchFile  = "ct-watch.json"
	addressCTFirstLeast = time.Minute
	addressCTFirstMost  = 20 * time.Minute
	addressCTRetryFirst = 10 * time.Minute
	addressCTRetryMost  = 24 * time.Hour
	addressCTPagesMost  = 20
)

// addressCTRequestTimeout is how long one search may take before the next is asked.
var addressCTRequestTimeout = 30 * time.Second

type addressCTSource struct {
	Kind string
	Base string
}

func addressCTSources() []addressCTSource {
	sources := []addressCTSource{}
	for _, part := range strings.Split(os.Getenv(addressCTSourcesEnv), ",") {
		kind, base, found := strings.Cut(strings.TrimSpace(part), ":")
		base = strings.TrimRight(strings.TrimSpace(base), "/")
		if found && base != "" && (kind == "crtsh" || kind == "certspotter") {
			sources = append(sources, addressCTSource{Kind: kind, Base: base})
		}
	}
	if len(sources) > 0 {
		return sources
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(addressCTSourceEnv)), "/")
	if base == "" {
		base = addressCTDefaultSource
	}
	return []addressCTSource{{Kind: "crtsh", Base: base}}
}

// addressCTWatch is what the server keeps of its watch: when the logs were last
// read for its name, when they last couldn't be and how many times in a row,
// and the search to ask first next time.
type addressCTWatch struct {
	CheckedAt int64 `json:"checkedAt,omitempty"`
	FailedAt  int64 `json:"failedAt,omitzero"`
	Failures  int   `json:"failures,omitempty"`
	Next      int   `json:"next,omitempty"`
}

// addressCTWatchStatus is the watch as the tunnel's screen shows it.
type addressCTWatchStatus struct {
	CheckedAt int64 `json:"checkedAt,omitempty"`
	FailedAt  int64 `json:"failedAt,omitzero"`
}

func readAddressCTWatch(dir string) addressCTWatch {
	watch := addressCTWatch{}
	if data, err := os.ReadFile(filepath.Join(dir, addressCTWatchFile)); err == nil {
		_ = json.Unmarshal(data, &watch)
	}
	return watch
}

func writeAddressCTWatch(dir string, watch addressCTWatch) error {
	data, err := json.Marshal(watch)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, addressCTWatchFile), data, 0o600)
}

// jitteredAddressCT is a span give or take a share of it.
func jitteredAddressCT(span time.Duration, share float64) time.Duration {
	return time.Duration(float64(span) * (1 - share + 2*share*rand.Float64()))
}

// addressCTRetryAfter is the wait after failures in a row: ten minutes, twice
// that each time, a day at most, give or take a fifth.
func addressCTRetryAfter(failures int) time.Duration {
	wait := addressCTRetryFirst
	for range max(failures-1, 0) {
		if wait >= addressCTRetryMost {
			break
		}
		wait *= 2
	}
	return jitteredAddressCT(min(wait, addressCTRetryMost), 0.2)
}

// ctSourceError is a search that didn't answer, and how long it asked to be
// left alone.
type ctSourceError struct {
	Source     string
	Status     int
	RetryAfter time.Duration
}

func (failure *ctSourceError) Error() string {
	return fmt.Sprintf("the certificate logs at %s didn't answer (%d)", failure.Source, failure.Status)
}

// ctAnswer reads a search's answer, or the error of one that refused.
func ctAnswer(response *http.Response, source string) ([]byte, error) {
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		failure := &ctSourceError{Source: source, Status: response.StatusCode}
		if seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After"))); err == nil && seconds > 0 {
			failure.RetryAfter = time.Duration(seconds) * time.Second
		} else if at, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
			failure.RetryAfter = time.Until(at)
		}
		return nil, failure
	}
	return io.ReadAll(io.LimitReader(response.Body, 16<<20))
}

// checkAddressCertificates asks the searches in turn, from the watch's next,
// until one answers; a search that is slow, refuses or answers oddly passes
// to the next. The longest wait any asked for comes back with the failure.
func checkAddressCertificates(ctx context.Context, dir, address string, sources []addressCTSource, watch *addressCTWatch) (*addressCertificateAlert, time.Duration, error) {
	var wait time.Duration
	var last error
	for offset := range sources {
		at := (watch.Next + offset) % len(sources)
		alert, err := findForeignAddressCertificate(ctx, dir, address, sources[at])
		if err == nil {
			watch.Next = (at + 1) % len(sources)
			return alert, 0, nil
		}
		var refused *ctSourceError
		if errors.As(err, &refused) && refused.RetryAfter > wait {
			wait = refused.RetryAfter
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, wait, last
}

// watchAddressCertificates looks at the logs while ctx lasts; a certificate the
// server didn't get ends the watch with onAlert.
func watchAddressCertificates(ctx context.Context, dir, address string, onAlert func(*addressCertificateAlert)) {
	wait := addressCTFirstLeast + time.Duration(rand.Int64N(int64(addressCTFirstMost-addressCTFirstLeast)))
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		watch := readAddressCTWatch(dir)
		alert, asked, err := checkAddressCertificates(ctx, dir, address, addressCTSources(), &watch)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			watch.Failures++
			watch.FailedAt = time.Now().UnixMilli()
			wait = max(addressCTRetryAfter(watch.Failures), asked)
		} else {
			watch.Failures, watch.CheckedAt = 0, time.Now().UnixMilli()
			wait = jitteredAddressCT(addressCTEvery, 0.25)
		}
		_ = writeAddressCTWatch(dir, watch)
		if alert != nil {
			onAlert(alert)
			return
		}
	}
}

// certSpotterIssuance is one certificate as Cert Spotter's search lists it.
type certSpotterIssuance struct {
	ID        string   `json:"id"`
	DNSNames  []string `json:"dns_names"`
	NotBefore string   `json:"not_before"`
	NotAfter  string   `json:"not_after"`
	CertDER   string   `json:"cert_der"`
	Issuer    struct {
		FriendlyName string `json:"friendly_name"`
	} `json:"issuer"`
}

// findForeignInCertSpotter asks Cert Spotter for the address with the
// wildcards that cover it, which holds the zone's too, a page at a time.
func findForeignInCertSpotter(ctx context.Context, client *http.Client, base, address string, issued addressIssued) (*addressCertificateAlert, error) {
	_, zone, _ := strings.Cut(address, ".")
	wildcard := "*." + zone
	after := ""
	for range addressCTPagesMost {
		query := url.Values{"domain": {address}, "match_wildcards": {"true"}, "expand": {"dns_names", "issuer", "cert_der"}}
		if after != "" {
			query.Set("after", after)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/issuances?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		data, err := ctAnswer(response, base)
		if err != nil {
			return nil, err
		}
		issuances := []certSpotterIssuance{}
		if err := json.Unmarshal(data, &issuances); err != nil {
			return nil, err
		}
		if len(issuances) == 0 {
			return nil, nil
		}
		for _, issuance := range issuances {
			names := make([]string, 0, len(issuance.DNSNames))
			for _, name := range issuance.DNSNames {
				names = append(names, strings.ToLower(name))
			}
			own, whole := slices.Contains(names, address), slices.Contains(names, wildcard)
			notBefore, errBefore := time.Parse(time.RFC3339, issuance.NotBefore)
			notAfter, errAfter := time.Parse(time.RFC3339, issuance.NotAfter)
			if (!own && !whole) || errBefore != nil || errAfter != nil || time.Now().After(notAfter) ||
				(!whole && notBefore.UnixMilli() < issued.Since-24*time.Hour.Milliseconds()) {
				continue
			}
			der, err := base64.StdEncoding.DecodeString(issuance.CertDER)
			if err != nil {
				return nil, err
			}
			certificate, err := x509.ParseCertificate(der)
			if err != nil {
				return nil, err
			}
			serial := normalSerial(certificate.SerialNumber.Text(16))
			if slices.Contains(issued.Serials, serial) {
				continue
			}
			name := address
			if whole {
				name = wildcard
			}
			entry, _ := strconv.ParseInt(issuance.ID, 10, 64)
			return &addressCertificateAlert{At: time.Now().UnixMilli(), Name: name, Issuer: issuance.Issuer.FriendlyName,
				Serial: serial, NotBefore: issuance.NotBefore, Entry: entry}, nil
		}
		after = issuances[len(issuances)-1].ID
	}
	return nil, nil
}
