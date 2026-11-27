//go:build windows && amd64

package main

import "math"

// putZoomFactor gives a WebView2 controller its page's size: on amd64 a
// double passes in the XMM register Go's Windows calls fill alongside the
// integer one.
func putZoomFactor(controller *comObject, zoom float64) bool {
	controller.call(controllerPutZoomFactor, uintptr(math.Float64bits(zoom)))
	return true
}
