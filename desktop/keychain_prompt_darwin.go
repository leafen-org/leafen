//go:build darwin && !ios

package main

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework Security
#include <CoreGraphics/CoreGraphics.h>
#include <Security/Security.h>

// Whether a window of SecurityAgent — the process macOS shows its keychain
// and other permission prompts in — is on screen. A window's owner is read
// without the screen-recording permission (its title is not).
static int leafenSystemPromptShowing(void) {
	CFArrayRef windows = CGWindowListCopyWindowInfo(
		kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (windows == NULL) return 0;
	int showing = 0;
	CFIndex count = CFArrayGetCount(windows);
	for (CFIndex i = 0; i < count && !showing; i++) {
		CFDictionaryRef window = (CFDictionaryRef)CFArrayGetValueAtIndex(windows, i);
		CFStringRef owner = (CFStringRef)CFDictionaryGetValue(window, kCGWindowOwnerName);
		if (owner != NULL && CFStringCompare(owner, CFSTR("SecurityAgent"), 0) == kCFCompareEqualTo) showing = 1;
	}
	CFRelease(windows);
	return showing;
}

// Reads, and drops, the key WebKit keeps in the keychain for the app (its
// WebCrypto master key, account "com.apple.WebKit.WebCrypto.master+<bundle
// ID>"), as WebKit reads it.
static void leafenReadWebKitKey(void) {
	CFStringRef bundle = CFBundleGetIdentifier(CFBundleGetMainBundle());
	if (bundle == NULL) return;
	CFStringRef account = CFStringCreateWithFormat(NULL, NULL, CFSTR("com.apple.WebKit.WebCrypto.master+%@"), bundle);
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(query, kSecAttrAccount, account);
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef result = NULL;
	if (SecItemCopyMatching(query, &result) == errSecSuccess && result != NULL) CFRelease(result);
	CFRelease(query);
	CFRelease(account);
}
*/
import "C"

func systemPromptShowing() bool { return C.leafenSystemPromptShowing() != 0 }

func readWebKitKey() { C.leafenReadWebKitKey() }
