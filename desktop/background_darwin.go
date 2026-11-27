//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement -framework IOKit -framework CoreFoundation
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#import <IOKit/pwr_mgt/IOPMLib.h>
#import <IOKit/ps/IOPowerSources.h>
#import <IOKit/ps/IOPSKeys.h>
#include <stdlib.h>

static int leafenLoginItemStatus(void) {
	if (@available(macOS 13.0, *)) {
		return (int)[SMAppService mainAppService].status;
	}
	return -1;
}

static int leafenLoginItemSet(int on) {
	if (@available(macOS 13.0, *)) {
		NSError *error = nil;
		BOOL done = on ? [[SMAppService mainAppService] registerAndReturnError:&error]
			: [[SMAppService mainAppService] unregisterAndReturnError:&error];
		return done ? 0 : (int)error.code;
	}
	return -1;
}

static int leafenOnBattery(void) {
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (!info) return 0;
	CFStringRef type = IOPSGetProvidingPowerSourceType(info);
	int battery = type && CFStringCompare(type, CFSTR(kIOPMBatteryPowerKey), 0) == kCFCompareEqualTo;
	CFRelease(info);
	return battery;
}

static unsigned int leafenHoldAwake(const char *reason) {
	IOPMAssertionID assertion = 0;
	CFStringRef name = CFStringCreateWithCString(NULL, reason, kCFStringEncodingUTF8);
	IOReturn result = IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleSystemSleep, kIOPMAssertionLevelOn, name, &assertion);
	CFRelease(name);
	return result == kIOReturnSuccess ? assertion : 0;
}

static void leafenReleaseAwake(unsigned int assertion) {
	if (assertion) IOPMAssertionRelease(assertion);
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// macOS opens the app at login as a login item of its own (SMAppService,
// macOS 13 and later), shown in System Settings → General → Login Items,
// and the app then starts behind its window (login_launch_darwin.go).
func loginItemState() string {
	switch C.leafenLoginItemStatus() {
	case 1:
		return "on"
	case 2:
		return "needs-approval"
	case -1:
		return "unavailable"
	}
	return "off"
}

func setLoginItem(on bool) error {
	flag := C.int(0)
	if on {
		flag = 1
	}
	if C.leafenLoginItemSet(flag) != 0 {
		return errors.New("macOS didn't change the login item")
	}
	return nil
}

func onBatteryPower() bool { return C.leafenOnBattery() != 0 }

// keepAwake holds a power assertion that keeps the computer from sleeping
// when idle; the display still sleeps.
type keepAwake struct{ assertion C.uint }

func (awake *keepAwake) hold(reason string) error {
	if awake.assertion != 0 {
		return nil
	}
	text := C.CString(reason)
	defer C.free(unsafe.Pointer(text))
	if awake.assertion = C.leafenHoldAwake(text); awake.assertion == 0 {
		return errors.New("macOS didn't take the assertion")
	}
	return nil
}

func (awake *keepAwake) release() {
	C.leafenReleaseAwake(awake.assertion)
	awake.assertion = 0
}
