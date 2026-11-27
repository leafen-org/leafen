//go:build darwin && !ios && cgo

package netproxy

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CFNetwork
#include <CoreFoundation/CoreFoundation.h>
#include <CFNetwork/CFNetwork.h>
#include <stdlib.h>
#include <string.h>

static char *leafenCopyString(CFStringRef value) {
	if (value == NULL) return strdup("");
	CFIndex size = CFStringGetMaximumSizeForEncoding(CFStringGetLength(value), kCFStringEncodingUTF8) + 1;
	char *out = malloc(size);
	if (!CFStringGetCString(value, out, size, kCFStringEncodingUTF8)) out[0] = 0;
	return out;
}

// leafenProxyLines writes a proxy list as lines: "DIRECT", "HTTP host
// port", "SOCKS host port", "PACURL address" or "PACJS".
static char *leafenProxyLines(CFArrayRef proxies) {
	CFMutableStringRef lines = CFStringCreateMutable(NULL, 0);
	CFIndex count = proxies == NULL ? 0 : CFArrayGetCount(proxies);
	for (CFIndex index = 0; index < count; index++) {
		CFDictionaryRef proxy = CFArrayGetValueAtIndex(proxies, index);
		CFStringRef type = CFDictionaryGetValue(proxy, kCFProxyTypeKey);
		if (type == NULL) continue;
		if (CFEqual(type, kCFProxyTypeNone)) {
			CFStringAppend(lines, CFSTR("DIRECT\n"));
		} else if (CFEqual(type, kCFProxyTypeHTTP) || CFEqual(type, kCFProxyTypeHTTPS) || CFEqual(type, kCFProxyTypeSOCKS)) {
			CFStringRef host = CFDictionaryGetValue(proxy, kCFProxyHostNameKey);
			CFNumberRef port = CFDictionaryGetValue(proxy, kCFProxyPortNumberKey);
			int number = 0;
			if (port != NULL) CFNumberGetValue(port, kCFNumberIntType, &number);
			if (host == NULL || number <= 0) continue;
			CFStringAppendFormat(lines, NULL, CFSTR("%s %@ %d\n"), CFEqual(type, kCFProxyTypeSOCKS) ? "SOCKS" : "HTTP", host, number);
		} else if (CFEqual(type, kCFProxyTypeAutoConfigurationURL)) {
			CFURLRef address = CFDictionaryGetValue(proxy, kCFProxyAutoConfigurationURLKey);
			if (address != NULL) CFStringAppendFormat(lines, NULL, CFSTR("PACURL %@\n"), CFURLGetString(address));
		} else if (CFEqual(type, kCFProxyTypeAutoConfigurationJavaScript)) {
			CFStringAppend(lines, CFSTR("PACJS\n"));
		}
	}
	char *out = leafenCopyString(lines);
	CFRelease(lines);
	return out;
}

static CFURLRef leafenURL(const char *target) {
	CFStringRef text = CFStringCreateWithCString(NULL, target, kCFStringEncodingUTF8);
	if (text == NULL) return NULL;
	CFURLRef address = CFURLCreateWithString(NULL, text, NULL);
	CFRelease(text);
	return address;
}

// leafenSystemProxies is the proxies the system's settings give an address.
static char *leafenSystemProxies(const char *target) {
	CFURLRef address = leafenURL(target);
	CFDictionaryRef settings = CFNetworkCopySystemProxySettings();
	if (address == NULL || settings == NULL) {
		if (address != NULL) CFRelease(address);
		if (settings != NULL) CFRelease(settings);
		return NULL;
	}
	CFArrayRef proxies = CFNetworkCopyProxiesForURL(address, settings);
	char *out = leafenProxyLines(proxies);
	if (proxies != NULL) CFRelease(proxies);
	CFRelease(settings);
	CFRelease(address);
	return out;
}

// leafenScriptProxies is the proxies an automatic configuration script
// gives an address, as the system evaluates it.
static char *leafenScriptProxies(const char *script, const char *target) {
	CFURLRef address = leafenURL(target);
	CFStringRef text = CFStringCreateWithCString(NULL, script, kCFStringEncodingUTF8);
	if (address == NULL || text == NULL) {
		if (address != NULL) CFRelease(address);
		if (text != NULL) CFRelease(text);
		return NULL;
	}
	CFErrorRef error = NULL;
	CFArrayRef proxies = CFNetworkCopyProxiesForAutoConfigurationScript(text, address, &error);
	char *out = NULL;
	if (proxies != NULL) {
		out = leafenProxyLines(proxies);
		CFRelease(proxies);
	}
	if (error != NULL) CFRelease(error);
	CFRelease(text);
	CFRelease(address);
	return out;
}
*/
import "C"

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"unsafe"
)

func init() { autoProxies = darwinAutoProxies }

var errAutoAnswer = errors.New("the automatic configuration gave no answer")

// darwinAutoProxies asks CFNetwork: the script at its address evaluated
// for the address, or, where the system finds its script itself, the
// system's own answer, a script it names fetched and evaluated.
func darwinAutoProxies(target *url.URL, script string, discover bool) ([]*url.URL, error) {
	if script != "" {
		return darwinScriptProxies(script, target, 0)
	}
	address := C.CString(target.String())
	defer C.free(unsafe.Pointer(address))
	lines := C.leafenSystemProxies(address)
	if lines == nil {
		return nil, errAutoAnswer
	}
	defer C.free(unsafe.Pointer(lines))
	return darwinLines(C.GoString(lines), target, 0)
}

func darwinScriptProxies(script string, target *url.URL, depth int) ([]*url.URL, error) {
	text, err := fetchAutoScript(script)
	if err != nil {
		return nil, err
	}
	return evaluateAutoScript(text, target, depth)
}

// evaluateAutoScript is the proxies a script gives an address, as the
// system evaluates it.
func evaluateAutoScript(text string, target *url.URL, depth int) ([]*url.URL, error) {
	script := C.CString(text)
	defer C.free(unsafe.Pointer(script))
	address := C.CString(target.String())
	defer C.free(unsafe.Pointer(address))
	lines := C.leafenScriptProxies(script, address)
	if lines == nil {
		return nil, errAutoAnswer
	}
	defer C.free(unsafe.Pointer(lines))
	return darwinLines(C.GoString(lines), target, depth)
}

// darwinLines reads the proxy lines, a script one names evaluated in turn.
func darwinLines(lines string, target *url.URL, depth int) ([]*url.URL, error) {
	proxies := []*url.URL{}
	for _, line := range strings.Split(strings.TrimSpace(lines), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "DIRECT":
			proxies = append(proxies, nil)
		case "HTTP":
			if len(fields) == 3 {
				// An HTTPS one is an HTTP proxy that HTTPS goes through.
				proxies = append(proxies, &url.URL{Scheme: "http", Host: net.JoinHostPort(fields[1], fields[2])})
			}
		case "PACURL":
			if len(fields) == 2 && depth == 0 {
				more, err := darwinScriptProxies(fields[1], target, depth+1)
				if err == nil {
					proxies = append(proxies, more...)
				}
			}
		}
	}
	if len(proxies) == 0 {
		return nil, errAutoAnswer
	}
	return proxies, nil
}
