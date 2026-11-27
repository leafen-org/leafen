//go:build !leafen_noapps

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// Sockets (docs/app-platform.md 12, T7): a worker reaches a TCP or TLS
// address its manifest's network.sockets lists — and so the administrator
// approved — through Leafen, which checks where the name resolves as the
// relay does (public only unless the entry was approved for the private
// network, never loopback, link-local, metadata or Leafen's own port) and
// records each connection: where, the bytes each way and how long. The
// worker reads and writes bytes through host functions, and a run's sockets
// close when the run ends.

const (
	appSocketsPerRun   = 4
	appSocketChunk     = 64 << 10
	appSocketWaitMax   = 30 * time.Second
	appSocketDialLimit = 10 * time.Second
)

type appSocket struct {
	conn           net.Conn
	address        string
	opened         time.Time
	sent, received int64
	outcome        string
	// The credentials its entry lets the run write to it.
	credentials []string
	// What it sent that waits, in case it is the start of a secret, or
	// past what the run asked to read at once.
	pending []byte
	// ended is how its stream ended (the other end closing it, or a failed
	// read), kept until what waits has gone out.
	ended error
}

// appSocketRoots are the roots a TLS socket trusts: the system's, or a
// test's own.
var appSocketRoots *x509.CertPool

type appSocketRequest struct {
	Socket int    `json:"socket"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	TLS    bool   `json:"tls"`
	Data   string `json:"data"`
	Max    int    `json:"max"`
	WaitMs int    `json:"waitMs"`
}

func appSocketAnswer(value any) ([]byte, error) { return json.Marshal(value) }

func appSocketRefusal(message string) ([]byte, error) {
	return appSocketAnswer(map[string]string{"error": message})
}

// socket answers leafen_socket_open, _write, _read and _close.
func (host *appWorkerHost) socket(name string, payload []byte) ([]byte, error) {
	request := appSocketRequest{}
	if err := json.Unmarshal(payload, &request); err != nil {
		return appSocketRefusal("a socket call takes JSON")
	}
	if name == "leafen_socket_open" {
		return host.openSocket(request)
	}
	opened := host.sockets[request.Socket]
	if opened == nil {
		return appSocketRefusal("no such socket")
	}
	switch name {
	case "leafen_socket_write":
		data, err := base64.StdEncoding.DecodeString(request.Data)
		if err != nil || len(data) > appSocketChunk {
			return appSocketRefusal("data is base64 of up to 64 KB")
		}
		if appMentionsCredential(string(data)) {
			filled, err := host.credentialFiller().fill(string(data), opened.credentials)
			if err != nil {
				return appSocketRefusal(err.Error())
			}
			data = []byte(filled)
		}
		_ = opened.conn.SetWriteDeadline(time.Now().Add(appSocketDialLimit))
		written, err := opened.conn.Write(data)
		opened.sent += int64(written)
		if err != nil {
			host.closeSocket(request.Socket, "write failed")
			return appSocketRefusal("the connection broke")
		}
		return appSocketAnswer(map[string]bool{"ok": true})
	case "leafen_socket_read":
		size := request.Max
		if size <= 0 || size > appSocketChunk {
			size = appSocketChunk
		}
		wait := time.Duration(request.WaitMs) * time.Millisecond
		if wait <= 0 || wait > appSocketWaitMax {
			wait = appSocketWaitMax
		}
		// The secrets the run filled never come back to it; what is held
		// back for that goes out first once it may. The socket is closed
		// only with the last of what waits, so a run reading a little at a
		// time gets all the other end sent before it closed.
		data := []byte(nil)
		if len(opened.pending) > 0 {
			data, opened.pending = host.credentials.pass(opened.pending, opened.ended != nil)
		}
		// Bytes held back are not an answer: the read goes on to the one
		// deadline the wait set, and answers once there is something to
		// hand out, the other end closes or the wait ends — so an empty
		// answer always means the wait passed, however small the reads
		// (2026-11-17: a run reading one byte at a time got empty answers
		// at once from the masked password on and stopped before the rest).
		if len(data) == 0 && opened.ended == nil {
			deadline := time.Now().Add(wait)
			_ = opened.conn.SetReadDeadline(deadline)
			buffer := make([]byte, size)
			for len(data) == 0 && opened.ended == nil {
				read, err := opened.conn.Read(buffer)
				opened.received += int64(read)
				if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
					opened.ended = err
				}
				data, opened.pending = host.credentials.pass(append(opened.pending, buffer[:read]...), opened.ended != nil)
				if errors.Is(err, os.ErrDeadlineExceeded) || !time.Now().Before(deadline) {
					break
				}
			}
		}
		if len(data) > size {
			opened.pending = append(append([]byte(nil), data[size:]...), opened.pending...)
			data = data[:size]
		}
		answer := map[string]any{"data": base64.StdEncoding.EncodeToString(data), "closed": false}
		if opened.ended != nil && len(opened.pending) == 0 {
			answer["closed"] = true
			if errors.Is(opened.ended, io.EOF) {
				host.closeSocket(request.Socket, "closed by the other end")
			} else {
				host.closeSocket(request.Socket, "read failed")
			}
		}
		return appSocketAnswer(answer)
	case "leafen_socket_close":
		host.closeSocket(request.Socket, "closed")
		return appSocketAnswer(map[string]bool{"ok": true})
	}
	return nil, errors.New("no such socket call")
}

// openSocket connects to an approved address the way the relay checks it.
func (host *appWorkerHost) openSocket(request appSocketRequest) ([]byte, error) {
	var entry *appManifestSocket
	// A worker names an address the administrator sets as {{setting:KEY}}.
	setting, named := appSettingName(request.Host)
	for index, candidate := range host.manifest.Network.Sockets {
		if named && candidate.Setting == setting && candidate.TLS == request.TLS ||
			strings.EqualFold(candidate.Host, strings.TrimSpace(request.Host)) && candidate.Port == request.Port && candidate.TLS == request.TLS {
			entry = &host.manifest.Network.Sockets[index]
		}
	}
	address := net.JoinHostPort(strings.ToLower(strings.TrimSpace(request.Host)), strconv.Itoa(request.Port))
	if named {
		address = request.Host
	}
	if entry != nil {
		address = net.JoinHostPort(entry.Host, strconv.Itoa(entry.Port))
	}
	if entry == nil {
		host.logSocket(&appSocket{address: address, opened: time.Now(), outcome: "not approved"})
		return appSocketRefusal("the address isn't among the app's approved sockets")
	}
	if len(host.sockets) >= appSocketsPerRun {
		return appSocketRefusal("a run keeps at most 4 sockets open")
	}
	ctx, cancel := context.WithTimeout(host.ctx, appSocketDialLimit)
	defer cancel()
	opened := &appSocket{address: address, opened: time.Now()}
	conn, err := appRelayDialContext(ctx, "tcp", address, entry.PrivateNetwork)
	if err == nil && entry.TLS {
		secured := tls.Client(conn, &tls.Config{ServerName: entry.Host, MinVersion: tls.VersionTLS12, RootCAs: appSocketRoots})
		if err = secured.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
		}
		conn = secured
	}
	if err != nil {
		opened.outcome = "refused"
		host.logSocket(opened)
		return appSocketRefusal("the address can't be reached")
	}
	opened.conn = conn
	opened.credentials = entry.Credentials
	host.nextSocket++
	if host.sockets == nil {
		host.sockets = map[int]*appSocket{}
	}
	host.sockets[host.nextSocket] = opened
	return appSocketAnswer(map[string]int{"socket": host.nextSocket})
}

func (host *appWorkerHost) closeSocket(id int, outcome string) {
	opened := host.sockets[id]
	if opened == nil {
		return
	}
	delete(host.sockets, id)
	_ = opened.conn.Close()
	opened.outcome = outcome
	host.logSocket(opened)
}

// closeSockets ends what a run left open.
func (host *appWorkerHost) closeSockets() {
	for id := range host.sockets {
		host.closeSocket(id, "closed when the run ended")
	}
}

func (host *appWorkerHost) logSocket(opened *appSocket) {
	_, _ = host.app.DB().Insert(appRelayLogTable, dbx.Params{
		"install_id": host.install.ID, "person_id": host.run.Subject, "method": "SOCKET", "host": opened.address, "path": "",
		"status": 0, "outcome": appOneLine(opened.outcome, 60), "request_bytes": opened.sent, "request_type": "",
		"response_bytes": opened.received, "duration_ms": time.Since(opened.opened).Milliseconds(), "created": opened.opened.UnixMilli(),
	}).Execute()
}
