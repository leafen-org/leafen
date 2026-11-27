//go:build windows

package main

import (
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows checks the device's owner with Windows Hello — the face,
// fingerprint or Hello PIN — through UserConsentVerifier, its prompt over
// the app's window. Where Hello isn't set up for the person, Windows asks
// for this account's password and the system checks it is this Windows
// account's (CredUI, then LogonUser and the two accounts' SIDs).

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")

	credui                         = windows.NewLazySystemDLL("credui.dll")
	procCredUIPromptForWindowsCred = credui.NewProc("CredUIPromptForWindowsCredentialsW")
	procCredUnPackAuthBuffer       = credui.NewProc("CredUnPackAuthenticationBufferW")

	advapi32      = windows.NewLazySystemDLL("advapi32.dll")
	procLogonUser = advapi32.NewProc("LogonUserW")
)

var (
	iidUserConsentVerifierStatics = mustGUID("{AF4F3F91-564C-4DDC-B8B8-973447627C65}")
	iidUserConsentVerifierInterop = mustGUID("{39E050C3-4E74-441A-8DC0-B81104DF949C}")
	iidAsyncInfo                  = mustGUID("{00000036-0000-0000-C000-000000000046}")
	// IAsyncOperation<UserConsentVerificationResult>, by WinRT's
	// parameterized interface ID.
	iidAsyncVerificationResult = mustGUID("{FD596FFD-2318-558F-9DBE-D21DF43764A5}")
)

const (
	helloAvailable       = 0 // UserConsentVerifierAvailability.Available
	helloVerified        = 0 // UserConsentVerificationResult.Verified
	helloDeviceNotFound  = 1
	helloNotConfigured   = 2
	helloDisabled        = 3
	asyncStarted         = 0
	asyncCompleted       = 1
	roInitMultithreaded  = 1
	rpcChangedMode       = 0x80010106
	errorCancelled       = 1223
	credPackProtected    = 0x1
	creduiwinCurrentUser = 0x200
	logonInteractive     = 2
	logonProviderDefault = 0
)

func mustGUID(text string) windows.GUID {
	guid, err := windows.GUIDFromString(text)
	if err != nil {
		panic(err)
	}
	return guid
}

// Hello or the account's password can always check the owner.
func deviceOwnerCheckAvailable() bool { return true }

func checkDeviceOwner(reason string) deviceOwnerCheck {
	if strings.TrimSpace(reason) == "" {
		reason = "Leafen"
	}
	window := uintptr(0)
	if links.window != nil {
		window = uintptr(links.window.NativeWindow())
	}
	if checked, usable := checkByHello(window, reason); usable {
		return checked
	}
	return checkByPassword(window, reason)
}

// comCall calls the method at index in a COM object's table.
func comCall(object unsafe.Pointer, index int, args ...uintptr) uintptr {
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, index*int(unsafe.Sizeof(uintptr(0)))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(object)}, args...)...)
	return result
}

func comRelease(object unsafe.Pointer) {
	if object != nil {
		comCall(object, 2)
	}
}

func hstring(text string) (uintptr, func(), error) {
	chars, err := windows.UTF16FromString(text)
	if err != nil {
		return 0, nil, err
	}
	var handle uintptr
	if hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1), uintptr(unsafe.Pointer(&handle))); hr != 0 {
		return 0, nil, syscall.Errno(hr)
	}
	return handle, func() { _, _, _ = procWindowsDeleteString.Call(handle) }, nil
}

// awaitResult waits for a WinRT operation the person answers and gives its
// 32-bit result, or false when it ended without one.
func awaitResult(operation unsafe.Pointer) (int32, bool) {
	var info unsafe.Pointer
	if comCall(operation, 0, uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info))) != 0 {
		return 0, false
	}
	defer comRelease(info)
	var status int32
	for {
		if comCall(info, 7, uintptr(unsafe.Pointer(&status))) != 0 {
			return 0, false
		}
		if status != asyncStarted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != asyncCompleted {
		return 0, false
	}
	var result int32
	if comCall(operation, 8, uintptr(unsafe.Pointer(&result))) != 0 {
		return 0, false
	}
	return result, true
}

// checkByHello asks Windows Hello over the window; usable is false where
// Hello isn't there for this person, so the password is asked instead.
func checkByHello(window uintptr, reason string) (deviceOwnerCheck, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procRoInitialize.Call(roInitMultithreaded); hr == 0 || hr == 1 {
		defer func() { _, _, _ = procRoUninitialize.Call() }()
	} else if uint32(hr) != rpcChangedMode {
		return deviceOwnerUnavailable, false
	}
	class, free, err := hstring("Windows.Security.Credentials.UI.UserConsentVerifier")
	if err != nil {
		return deviceOwnerUnavailable, false
	}
	defer free()
	var statics unsafe.Pointer
	if hr, _, _ := procRoGetActivationFactory.Call(class, uintptr(unsafe.Pointer(&iidUserConsentVerifierStatics)), uintptr(unsafe.Pointer(&statics))); hr != 0 {
		return deviceOwnerUnavailable, false
	}
	defer comRelease(statics)
	var availability unsafe.Pointer
	if comCall(statics, 6, uintptr(unsafe.Pointer(&availability))) != 0 {
		return deviceOwnerUnavailable, false
	}
	available, ok := awaitResult(availability)
	comRelease(availability)
	if !ok || available != helloAvailable {
		return deviceOwnerUnavailable, false
	}
	var interop unsafe.Pointer
	if comCall(statics, 0, uintptr(unsafe.Pointer(&iidUserConsentVerifierInterop)), uintptr(unsafe.Pointer(&interop))) != 0 {
		return deviceOwnerUnavailable, false
	}
	defer comRelease(interop)
	message, freeMessage, err := hstring(reason)
	if err != nil {
		return deviceOwnerUnavailable, false
	}
	defer freeMessage()
	var verification unsafe.Pointer
	if comCall(interop, 6, window, message, uintptr(unsafe.Pointer(&iidAsyncVerificationResult)), uintptr(unsafe.Pointer(&verification))) != 0 {
		return deviceOwnerUnavailable, false
	}
	defer comRelease(verification)
	result, ok := awaitResult(verification)
	switch {
	case ok && result == helloVerified:
		return deviceOwnerChecked, true
	case ok && (result == helloDeviceNotFound || result == helloNotConfigured || result == helloDisabled):
		return deviceOwnerUnavailable, false
	}
	return deviceOwnerCancelled, true
}

type credUIInfo struct {
	size    uint32
	parent  uintptr
	message *uint16
	caption *uint16
	banner  uintptr
}

// checkByPassword asks for this Windows account's password and checks with
// the system that it signs in this same account.
func checkByPassword(window uintptr, reason string) deviceOwnerCheck {
	caption, _ := windows.UTF16PtrFromString("Leafen")
	message, err := windows.UTF16PtrFromString(reason)
	if err != nil {
		return deviceOwnerUnavailable
	}
	info := credUIInfo{parent: window, message: message, caption: caption}
	info.size = uint32(unsafe.Sizeof(info))
	var pack uint32
	var buffer unsafe.Pointer
	var size uint32
	var save int32
	answer, _, _ := procCredUIPromptForWindowsCred.Call(uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&pack)), 0, 0,
		uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&save)), creduiwinCurrentUser)
	if answer == errorCancelled {
		return deviceOwnerCancelled
	}
	if answer != 0 || buffer == nil {
		return deviceOwnerUnavailable
	}
	defer func() {
		clear(unsafe.Slice((*byte)(buffer), size))
		windows.CoTaskMemFree(buffer)
	}()
	user, domain, password := make([]uint16, 514), make([]uint16, 256), make([]uint16, 256)
	defer clear(password)
	userLen, domainLen, passwordLen := uint32(len(user)), uint32(len(domain)), uint32(len(password))
	if ok, _, _ := procCredUnPackAuthBuffer.Call(credPackProtected, uintptr(buffer), uintptr(size),
		uintptr(unsafe.Pointer(&user[0])), uintptr(unsafe.Pointer(&userLen)),
		uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&domainLen)),
		uintptr(unsafe.Pointer(&password[0])), uintptr(unsafe.Pointer(&passwordLen))); ok == 0 {
		return deviceOwnerCancelled
	}
	name, place := windows.UTF16ToString(user), windows.UTF16ToString(domain)
	if place == "" {
		if at := strings.IndexByte(name, '\\'); at > 0 {
			name, place = name[at+1:], name[:at]
		}
	}
	namePtr, _ := windows.UTF16PtrFromString(name)
	var placePtr *uint16
	if place != "" {
		placePtr, _ = windows.UTF16PtrFromString(place)
	}
	var token windows.Token
	if ok, _, _ := procLogonUser.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(placePtr)), uintptr(unsafe.Pointer(&password[0])),
		logonInteractive, logonProviderDefault, uintptr(unsafe.Pointer(&token))); ok == 0 {
		return deviceOwnerCancelled
	}
	defer token.Close()
	if sameAccount(token) {
		return deviceOwnerChecked
	}
	return deviceOwnerCancelled
}

// sameAccount says whether a signed-in token is this app's own account.
func sameAccount(token windows.Token) bool {
	current, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer current.Close()
	mine, err := current.GetTokenUser()
	if err != nil {
		return false
	}
	theirs, err := token.GetTokenUser()
	return err == nil && mine.User.Sid.Equals(theirs.User.Sid)
}
