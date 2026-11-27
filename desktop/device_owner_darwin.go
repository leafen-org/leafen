//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework LocalAuthentication -framework Foundation
#include <stdlib.h>
#import <LocalAuthentication/LocalAuthentication.h>

// leafenDeviceOwnerAvailable is 1 where the system can check the device's
// owner: Touch ID or Face ID, else the device's password.
static int leafenDeviceOwnerAvailable(void) {
	LAContext *context = [[LAContext alloc] init];
	NSError *error = nil;
	return [context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&error] ? 1 : 0;
}

// leafenCheckDeviceOwner asks the system to check the owner, showing
// reason, and waits: 1 checked, 0 cancelled or failed, -1 unavailable.
static int leafenCheckDeviceOwner(const char *reason) {
	LAContext *context = [[LAContext alloc] init];
	NSError *error = nil;
	if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&error]) return -1;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	__block int result = 0;
	[context evaluatePolicy:LAPolicyDeviceOwnerAuthentication
		localizedReason:[NSString stringWithUTF8String:reason]
		reply:^(BOOL success, NSError *failure) {
			result = success ? 1 : 0;
			dispatch_semaphore_signal(done);
		}];
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	return result;
}
*/
import "C"

import (
	"strings"
	"unsafe"
)

// macOS and iOS check the device's owner with LocalAuthentication.

func deviceOwnerCheckAvailable() bool { return C.leafenDeviceOwnerAvailable() == 1 }

func checkDeviceOwner(reason string) deviceOwnerCheck {
	if strings.TrimSpace(reason) == "" {
		reason = "Leafen"
	}
	text := C.CString(reason)
	defer C.free(unsafe.Pointer(text))
	switch C.leafenCheckDeviceOwner(text) {
	case 1:
		return deviceOwnerChecked
	case -1:
		return deviceOwnerUnavailable
	}
	return deviceOwnerCancelled
}
