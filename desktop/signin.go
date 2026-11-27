package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"sync"
	"time"
)

// A sign-in on another Leafen server through its providers runs in the
// system's browser, as RFC 8252 has native apps do, and comes back to the
// app: on a desktop to a loopback address the app listens on for that
// sign-in only, on iOS to the app's own scheme through the system's
// sign-in sheet. The page names the server's sign-in address with the
// return address Begin gives it, Open shows it, and Wait gives the page
// what came back — the query the server's completion carried — so the page
// can exchange it with its PKCE verifier. Android's page does this through
// the app's own bridge (LeafenSignIn.java).

// signInReturnScheme is the app's own scheme a server may send a sign-in to.
const signInReturnScheme = "org.leafen.app"

// signInWaitLimit bounds how long one sign-in waits for its return.
const signInWaitLimit = 15 * time.Minute

var errSignInUnavailable = errors.New("this device's app can't bring a login back")

// SignInReturn is the app's side of a provider sign-in on another server.
type SignInReturn struct {
	mu      sync.Mutex
	waiting map[string]*signInWait
}

// BegunSignIn is a sign-in waiting for its return: its ID and where the
// server should send it.
type BegunSignIn struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type signInWait struct {
	result chan string
	stop   func()
	once   sync.Once
}

func newSignInReturn() *SignInReturn {
	return &SignInReturn{waiting: map[string]*signInWait{}}
}

func signInID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Begin starts waiting for one sign-in's return.
func (s *SignInReturn) Begin() (BegunSignIn, error) {
	id, err := signInID()
	if err != nil {
		return BegunSignIn{}, err
	}
	wait := &signInWait{result: make(chan string, 1)}
	address, stop, err := listenForSignIn(id, func(query string) { s.finish(id, query) })
	if err != nil {
		return BegunSignIn{}, err
	}
	wait.stop = stop
	s.mu.Lock()
	s.waiting[id] = wait
	s.mu.Unlock()
	time.AfterFunc(signInWaitLimit, func() {
		s.finish(id, "")
		s.forget(id)
	})
	return BegunSignIn{ID: id, Address: address}, nil
}

// Open shows a server's sign-in address, an http(s) one, for a sign-in begun.
func (s *SignInReturn) Open(id, address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return errors.New("only a server's own login address opens")
	}
	s.mu.Lock()
	_, waiting := s.waiting[id]
	s.mu.Unlock()
	if !waiting {
		return errors.New("no such login")
	}
	return openSignIn(id, address, func(query string) { s.finish(id, query) })
}

// Wait gives what a sign-in brought back, the query of its return, or ""
// when it ended without one; a return that came first is kept for it.
func (s *SignInReturn) Wait(id string) string {
	s.mu.Lock()
	wait := s.waiting[id]
	s.mu.Unlock()
	if wait == nil {
		return ""
	}
	query := <-wait.result
	s.forget(id)
	return query
}

// Cancel ends a sign-in's wait without a return.
func (s *SignInReturn) Cancel(id string) {
	s.finish(id, "")
}

func (s *SignInReturn) forget(id string) {
	s.mu.Lock()
	delete(s.waiting, id)
	s.mu.Unlock()
}

func (s *SignInReturn) finish(id, query string) {
	s.mu.Lock()
	wait := s.waiting[id]
	s.mu.Unlock()
	if wait == nil {
		return
	}
	wait.once.Do(func() {
		if wait.stop != nil {
			wait.stop()
		}
		wait.result <- query
		close(wait.result)
	})
}
