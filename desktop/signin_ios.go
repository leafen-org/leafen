//go:build ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AuthenticationServices -framework UIKit -framework Foundation
#include <stdlib.h>

void leafenStartSignIn(const char *identifier, const char *address, const char *scheme);
*/
import "C"

import (
	"net/url"
	"sync"
	"unsafe"
)

// On iOS the sign-in shows in the system's sign-in sheet
// (ASWebAuthenticationSession), which ends at the app's own scheme
// (RFC 8252 7.1); nothing listens for it.

var iosSignIns sync.Map // ID → func(query string)

func listenForSignIn(id string, returned func(string)) (string, func(), error) {
	iosSignIns.Store(id, returned)
	return signInReturnScheme + ":/sign-in/" + id, func() { iosSignIns.Delete(id) }, nil
}

func openSignIn(id string, address string, _ func(string)) error {
	identifier, target, scheme := C.CString(id), C.CString(address), C.CString(signInReturnScheme)
	defer C.free(unsafe.Pointer(identifier))
	defer C.free(unsafe.Pointer(target))
	defer C.free(unsafe.Pointer(scheme))
	C.leafenStartSignIn(identifier, target, scheme)
	return nil
}

//export leafenSignInReturned
func leafenSignInReturned(identifier *C.char, address *C.char) {
	id, returned := C.GoString(identifier), C.GoString(address)
	finish, ok := iosSignIns.Load(id)
	if !ok {
		return
	}
	query := ""
	if parsed, err := url.Parse(returned); err == nil && parsed.Scheme == signInReturnScheme {
		query = parsed.RawQuery
	}
	finish.(func(string))(query)
}
