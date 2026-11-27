package main

import (
	"net"
	"sync"
	"time"
)

// reopeningListener accepts at the app server's loopback address and listens
// there again when the socket stopped taking connections without the app
// closing it, as iOS may do to a suspended app's listening socket (Apple
// TN2277), so the window finds its server after the app resumes.
type reopeningListener struct {
	mu      sync.Mutex
	inner   net.Listener
	address string
	closed  bool
}

func newReopeningListener(inner net.Listener) *reopeningListener {
	return &reopeningListener{inner: inner, address: inner.Addr().String()}
}

func (listener *reopeningListener) current() (net.Listener, bool) {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	return listener.inner, listener.closed
}

// takesConnections reports whether the address still answers; a connection
// made here waits in the socket's queue and its next Accept takes it.
func (listener *reopeningListener) takesConnections() bool {
	conn, err := net.DialTimeout("tcp", listener.address, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (listener *reopeningListener) Accept() (net.Conn, error) {
	for {
		inner, closed := listener.current()
		if closed {
			return nil, net.ErrClosed
		}
		conn, err := inner.Accept()
		if err == nil {
			return conn, nil
		}
		if _, closed := listener.current(); closed {
			return nil, net.ErrClosed
		}
		// A socket that still takes connections had a passing failure, which
		// the server retries itself.
		if listener.takesConnections() {
			return nil, err
		}
		listener.reopen(inner)
	}
}

// reopen listens at the address again, waiting while it can't.
func (listener *reopeningListener) reopen(dead net.Listener) {
	_ = dead.Close()
	for {
		listener.mu.Lock()
		if listener.closed || listener.inner != dead {
			listener.mu.Unlock()
			return
		}
		next, err := net.Listen("tcp", listener.address)
		if err == nil {
			listener.inner = next
			listener.mu.Unlock()
			return
		}
		listener.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func (listener *reopeningListener) Close() error {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	listener.closed = true
	return listener.inner.Close()
}

func (listener *reopeningListener) Addr() net.Addr {
	inner, _ := listener.current()
	return inner.Addr()
}
