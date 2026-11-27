//go:build !leafen_noapps

package app

import (
	"fmt"
	"log/slog"
)

// answerAppWorkerHost answers one of a worker's host calls. A host call that
// breaks answers the worker with an error, as a refusal does, so the run
// goes on to its end — the worker's own failure, its record closed with its
// reason and its place given back (2026-11-16: a call that broke midway left
// its run "running" and its app's place taken until the server restarted,
// and two such runs made the app "busy" for everyone).
func answerAppWorkerHost(host func(string, []byte) ([]byte, error), name string, payload []byte) (result []byte, err error) {
	defer func() {
		if broke := recover(); broke != nil {
			slog.Error("leafen app host call broke", "call", name, "panic", fmt.Sprint(broke))
			result, err = nil, fmt.Errorf("%s couldn't be answered", name)
		}
	}()
	return host(name, payload)
}
