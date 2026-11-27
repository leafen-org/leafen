//go:build ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework UIKit -framework Foundation

void leafenStartCodeScan(void);
*/
import "C"

import "sync"

// The scan waiting for the camera (code_scanner_ios.m); one at a time.
var codeScanWaiting struct {
	sync.Mutex
	answer chan CodeScan
}

func scanCode() CodeScan {
	codeScanWaiting.Lock()
	if codeScanWaiting.answer != nil {
		codeScanWaiting.Unlock()
		return CodeScan{Outcome: "cancelled"}
	}
	answer := make(chan CodeScan, 1)
	codeScanWaiting.answer = answer
	codeScanWaiting.Unlock()
	C.leafenStartCodeScan()
	return <-answer
}

//export leafenCodeScanned
func leafenCodeScanned(outcome *C.char, text *C.char) {
	codeScanWaiting.Lock()
	answer := codeScanWaiting.answer
	codeScanWaiting.answer = nil
	codeScanWaiting.Unlock()
	if answer != nil {
		answer <- CodeScan{Outcome: C.GoString(outcome), Text: C.GoString(text)}
	}
}
