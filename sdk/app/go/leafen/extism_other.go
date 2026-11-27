//go:build !wasip1

package leafen

import "errors"

// Outside a Leafen worker (GOOS=wasip1 GOARCH=wasm) there is no host: these
// let a worker's other code build and be tested on its own.

var errNoHost = errors.New("leafen: runs only inside a Leafen worker (GOOS=wasip1 GOARCH=wasm)")

// Input returns nothing outside a worker.
func Input() []byte { return nil }

// Output does nothing outside a worker.
func Output([]byte) {}

// Fail returns 1 outside a worker.
func Fail(string) int32 { return 1 }

func hostCall(hostFunction, []byte) ([]byte, error) { return nil, errNoHost }
