//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <stdlib.h>
#include <Security/Security.h>

// A generic password item of the app's service, for one account.
static CFMutableDictionaryRef leafenItemQuery(const char *service, const char *account) {
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFStringRef serviceRef = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
	CFStringRef accountRef = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(query, kSecAttrService, serviceRef);
	CFDictionarySetValue(query, kSecAttrAccount, accountRef);
	CFRelease(serviceRef);
	CFRelease(accountRef);
	return query;
}

static OSStatus leafenKeychainLoad(const char *service, const char *account, char **secret, int *length) {
	CFMutableDictionaryRef query = leafenItemQuery(service, account);
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	CFRelease(query);
	if (status != errSecSuccess) return status;
	CFDataRef data = (CFDataRef)result;
	*length = (int)CFDataGetLength(data);
	*secret = malloc(*length > 0 ? *length : 1);
	CFDataGetBytes(data, CFRangeMake(0, *length), (UInt8 *)*secret);
	CFRelease(result);
	return errSecSuccess;
}

static OSStatus leafenKeychainSave(const char *service, const char *account, const char *secret, int length) {
	CFDataRef data = CFDataCreate(NULL, (const UInt8 *)secret, length);
	CFMutableDictionaryRef query = leafenItemQuery(service, account);
	CFMutableDictionaryRef update = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(update, kSecValueData, data);
	OSStatus status = SecItemUpdate(query, update);
	if (status == errSecItemNotFound) {
		CFDictionarySetValue(query, kSecValueData, data);
		// Readable once the device is first unlocked, and never restored to
		// another device.
		CFDictionarySetValue(query, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly);
		status = SecItemAdd(query, NULL);
	}
	CFRelease(update);
	CFRelease(query);
	CFRelease(data);
	return status;
}

static OSStatus leafenKeychainForget(const char *service, const char *account) {
	CFMutableDictionaryRef query = leafenItemQuery(service, account);
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// keychainStore keeps sign-ins in the Keychain (macOS and iOS).
type keychainStore struct{}

func platformSecretStore() secretStore { return keychainStore{} }

func keychainError(status C.OSStatus) error {
	return fmt.Errorf("keychain status %d", int(status))
}

func (keychainStore) load(account string) (string, bool, error) {
	service, name := C.CString(credentialService), C.CString(account)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(name))
	var secret *C.char
	var length C.int
	status := C.leafenKeychainLoad(service, name, &secret, &length)
	if status == C.errSecItemNotFound {
		return "", false, nil
	}
	if status != C.errSecSuccess {
		return "", false, keychainError(status)
	}
	defer C.free(unsafe.Pointer(secret))
	return C.GoStringN(secret, length), true, nil
}

func (keychainStore) save(account, secret string) error {
	service, name, value := C.CString(credentialService), C.CString(account), C.CString(secret)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(value))
	if status := C.leafenKeychainSave(service, name, value, C.int(len(secret))); status != C.errSecSuccess {
		return keychainError(status)
	}
	return nil
}

func (keychainStore) forget(account string) error {
	service, name := C.CString(credentialService), C.CString(account)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(name))
	status := C.leafenKeychainForget(service, name)
	if status != C.errSecSuccess && status != C.errSecItemNotFound {
		return keychainError(status)
	}
	return nil
}
