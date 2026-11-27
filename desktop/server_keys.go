package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ServerKeys reads which server the window reaches at an address whose DNS
// someone else answers (Tailscale's ts.net): the window
// remembers the server's own identity there and refuses another later
// (decided 2026-10-17, moved to the server's signed identity 2026-10-19 —
// Tailscale makes a new certificate key at each renewal). The window's own
// connection checks the certificate; this reads, over one connection, the
// certificate key the server shows and the identity that signed it.
type ServerKeys struct{}

func newServerKeys() *ServerKeys { return &ServerKeys{} }

// errUnvouched is a server answering with a certificate its identity didn't
// sign: someone may stand in the middle.
const errUnvouched = "unvouched: the server didn't sign the certificate it shows"

// Identity answers the identity key (base64url Ed25519) of the server at an
// https origin, once it signed, for this request's nonce and over this very
// connection, the certificate key the connection shows. An answer without
// that is errUnvouched; a server that can't be reached is another error.
func (*ServerKeys) Identity(origin string) (string, error) {
	conn, _, identity, err := vouchedConnection(origin, false)
	if conn != nil {
		conn.Close()
	}
	return identity, err
}

// vouchedConnection reads, over one connection to an https origin, the
// identity that signed the certificate key the connection shows. With keep,
// the connection and its reader stay open for one more request on it (a
// phone's background refresh asks there once the identity is the one
// remembered); otherwise the server closes it.
func vouchedConnection(origin string, keep bool) (*tls.Conn, *bufio.Reader, string, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, nil, "", errors.New("only an https server has an identity here")
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp",
		net.JoinHostPort(host, port), &tls.Config{ServerName: host, InsecureSkipVerify: true, NextProtos: []string{"http/1.1"}})
	if err != nil {
		return nil, nil, "", err
	}
	identity, reader, err := vouch(conn, parsed.Host, host, keep)
	if err != nil {
		conn.Close()
		return nil, nil, "", err
	}
	return conn, reader, identity, nil
}

func vouch(conn *tls.Conn, authority, host string, keep bool) (string, *bufio.Reader, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	certificates := conn.ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		return "", nil, errors.New("the server showed no certificate")
	}
	sum := sha256.Sum256(certificates[0].RawSubjectPublicKeyInfo)
	shown := base64.RawURLEncoding.EncodeToString(sum[:])
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", nil, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(random)
	request, err := http.NewRequest(http.MethodGet, "https://"+authority+"/api/leafen/v1/server-identity?nonce="+nonce, nil)
	if err != nil {
		return "", nil, err
	}
	request.Close = !keep
	if err := request.Write(conn); err != nil {
		return "", nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, errors.New(errUnvouched)
	}
	answer := struct {
		Key       string   `json:"key"`
		Pins      []string `json:"pins"`
		Signature string   `json:"signature"`
	}{}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&answer); err != nil {
		return "", nil, errors.New(errUnvouched)
	}
	// The rest of the answer is read, so the connection is ready for another request.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	key, keyErr := base64.RawURLEncoding.DecodeString(answer.Key)
	signature, signatureErr := base64.RawURLEncoding.DecodeString(answer.Signature)
	message := "leafen-identity/1\n" + host + "\n" + nonce + "\n" + strings.Join(answer.Pins, ",")
	if keyErr != nil || signatureErr != nil || len(key) != ed25519.PublicKeySize ||
		!ed25519.Verify(ed25519.PublicKey(key), []byte(message), signature) || !slices.Contains(answer.Pins, shown) {
		return "", nil, errors.New(errUnvouched)
	}
	return answer.Key, reader, nil
}
