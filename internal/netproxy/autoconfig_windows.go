//go:build windows

package netproxy

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinHTTP evaluates the automatic configuration, as Windows' own programs
// do: the script at its address, or one the network gives (WPAD, by DHCP
// and DNS).

var (
	winhttp            = windows.NewLazySystemDLL("winhttp.dll")
	procIEProxyConfig  = winhttp.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	procOpen           = winhttp.NewProc("WinHttpOpen")
	procSetTimeouts    = winhttp.NewProc("WinHttpSetTimeouts")
	procGetProxyForURL = winhttp.NewProc("WinHttpGetProxyForUrl")
	procGlobalFree     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
)

const (
	winhttpAutoDetect      = 0x1
	winhttpConfigURL       = 0x2
	winhttpDetectDHCP      = 0x1
	winhttpDetectDNS       = 0x2
	winhttpAccessNoProxy   = 1
	winhttpAccessNamed     = 3
	winhttpDetectionFailed = 12180
	// autoDetectRest is how long a network that gave no script isn't asked
	// again: each asking waits on DHCP and DNS.
	autoDetectRest = 10 * time.Minute
)

type winhttpIEProxyConfig struct {
	autoDetect    int32
	autoConfigURL *uint16
	proxy         *uint16
	proxyBypass   *uint16
}

type winhttpAutoProxyOptions struct {
	flags              uint32
	autoDetectFlags    uint32
	autoConfigURL      *uint16
	reserved           uintptr
	reservedNumber     uint32
	autoLogonChallenge int32
}

type winhttpProxyInfo struct {
	accessType  uint32
	proxy       *uint16
	proxyBypass *uint16
}

func init() { autoProxies = windowsAutoProxies }

var (
	winhttpOnce      sync.Once
	winhttpSession   uintptr
	detectMu         sync.Mutex
	detectFailedAt   time.Time
	errAutoAnswerWin = errors.New("the automatic configuration gave no answer")
)

// autoDetectOn says whether the person's Internet Settings find a script
// on the network ("Automatically detect settings").
func autoDetectOn() bool {
	if procIEProxyConfig.Find() != nil {
		return false
	}
	config := winhttpIEProxyConfig{}
	if ok, _, _ := procIEProxyConfig.Call(uintptr(unsafe.Pointer(&config))); ok == 0 {
		return false
	}
	for _, text := range []*uint16{config.autoConfigURL, config.proxy, config.proxyBypass} {
		if text != nil {
			_, _, _ = procGlobalFree.Call(uintptr(unsafe.Pointer(text)))
		}
	}
	return config.autoDetect != 0
}

func winhttpHandle() uintptr {
	winhttpOnce.Do(func() {
		if procOpen.Find() != nil {
			return
		}
		agent, _ := windows.UTF16PtrFromString("Leafen")
		handle, _, _ := procOpen.Call(uintptr(unsafe.Pointer(agent)), winhttpAccessNoProxy, 0, 0, 0)
		if handle != 0 {
			// Finding and reading a script waits a few seconds at most.
			_, _, _ = procSetTimeouts.Call(handle, 5000, 5000, 5000, 5000)
		}
		winhttpSession = handle
	})
	return winhttpSession
}

func windowsAutoProxies(target *url.URL, script string, discover bool) ([]*url.URL, error) {
	handle := winhttpHandle()
	if handle == 0 {
		return nil, errAutoAnswerWin
	}
	detectMu.Lock()
	if discover && time.Since(detectFailedAt) < autoDetectRest {
		discover = false
	}
	detectMu.Unlock()
	options := winhttpAutoProxyOptions{autoLogonChallenge: 1}
	if discover {
		options.flags |= winhttpAutoDetect
		options.autoDetectFlags = winhttpDetectDHCP | winhttpDetectDNS
	}
	if script != "" {
		address, err := windows.UTF16PtrFromString(script)
		if err != nil {
			return nil, errAutoAnswerWin
		}
		options.flags |= winhttpConfigURL
		options.autoConfigURL = address
	}
	if options.flags == 0 {
		return nil, errAutoAnswerWin
	}
	address, err := windows.UTF16PtrFromString(target.String())
	if err != nil {
		return nil, errAutoAnswerWin
	}
	info := winhttpProxyInfo{}
	ok, _, callErr := procGetProxyForURL.Call(handle, uintptr(unsafe.Pointer(address)), uintptr(unsafe.Pointer(&options)), uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errno, isErrno := callErr.(windows.Errno); isErrno && errno == winhttpDetectionFailed && discover {
			detectMu.Lock()
			detectFailedAt = time.Now()
			detectMu.Unlock()
			if script != "" {
				return windowsAutoProxies(target, script, false)
			}
		}
		return nil, errAutoAnswerWin
	}
	defer func() {
		for _, text := range []*uint16{info.proxy, info.proxyBypass} {
			if text != nil {
				_, _, _ = procGlobalFree.Call(uintptr(unsafe.Pointer(text)))
			}
		}
	}()
	if info.accessType != winhttpAccessNamed || info.proxy == nil {
		return []*url.URL{nil}, nil
	}
	return windowsProxyList(windows.UTF16PtrToString(info.proxy)), nil
}

// windowsProxyList reads WinHTTP's proxy list: "host:port" entries split by
// ";" or spaces, "http=" or "https=" before one naming what it is for.
func windowsProxyList(list string) []*url.URL {
	proxies := []*url.URL{}
	for _, entry := range strings.FieldsFunc(list, func(r rune) bool { return r == ';' || r == ' ' || r == '\t' }) {
		if scheme, address, named := strings.Cut(entry, "="); named {
			if !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
				continue
			}
			entry = address
		}
		if entry != "" {
			proxies = append(proxies, &url.URL{Scheme: "http", Host: entry})
		}
	}
	if len(proxies) == 0 {
		return []*url.URL{nil}
	}
	return proxies
}
