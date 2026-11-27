//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

// WebView2 tells the window of every new-window request in its page or any
// frame in it — a link meant for a new tab, a Ctrl- or middle-pressed link,
// window.open — and opens its own pop-up window on the address when no one
// answers, as Wails doesn't (v3.0.0-beta.27 to v3.1.0); and of every time its page
// goes elsewhere, as a pressed link without a target — in the page, or a
// frame's link to the top — takes it. The window's WebView2 is answered once
// its page has loaded, and again when Wails has made a new one after its
// browser process ended: a new window on an address outside the app or one
// of the app's pages goes by the app's rule (new_window.go), a window the
// page fills itself is WebView2's pop-up, and anything else opens nothing;
// the app's page taken to an address outside the app opens that by the rule
// instead and stays, as a pressed link does in the Mac app
// (new_window_darwin.m).
func routeNewWindows(window *application.WebviewWindow) {
	handler := &newWindowHandler{window: window}
	handler.newWindow = webViewAnswer{methods: &newWindowAnswerMethods, iid: &iidNewWindowRequestHandler, handler: handler}
	handler.navigation = webViewAnswer{methods: &navigationAnswerMethods, iid: &iidNavigationStartingHandler, handler: handler}
	window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		application.InvokeAsync(handler.listen)
	})
}

// windowOpen says whether an app window is open, on screen or minimized.
func windowOpen(window application.Window) bool {
	return window.IsVisible() || window.IsMinimised()
}

// Places in the COM method tables of WebView2.h.
const (
	coreWebView2GetSource             = 4
	coreWebView2AddNavigationStarting = 7
	coreWebView2AddNewWindowRequested = 44
	newWindowArgsGetURI               = 3
	newWindowArgsPutHandled           = 6
	navigationArgsGetURI              = 3
	navigationArgsGetIsRedirected     = 5
	navigationArgsPutCancel           = 8
	comNoInterface                    = 0x80004002
)

var (
	iidUnknown                 = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidNewWindowRequestHandler = windows.GUID{Data1: 0xd4c185fe, Data2: 0xc81c, Data3: 0x4989,
		Data4: [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51}}
	iidNavigationStartingHandler = windows.GUID{Data1: 0x9adbe429, Data2: 0xf36d, Data3: 0x432b,
		Data4: [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
)

// comObject is a COM object WebView2 made, whose first word points to its methods.
type comObject struct{ methods unsafe.Pointer }

func (object *comObject) method(place int) uintptr {
	return *(*uintptr)(unsafe.Add(object.methods, place*int(unsafe.Sizeof(uintptr(0)))))
}

// newWindowHandler is the window's answers to its WebView2. WebView2 holds
// them as long as the window, and so does newWindowHandlers.
type newWindowHandler struct {
	newWindow, navigation webViewAnswer
	window                *application.WebviewWindow
	// The holder of the WebView2 it answers, Wails' own; kept so that a new
	// holder, made with a new WebView2, is never at the same address.
	holder unsafe.Pointer
}

// webViewAnswer is one answer, a COM object whose first word is its methods:
// an ICoreWebView2NewWindowRequestedEventHandler or an
// ICoreWebView2NavigationStartingEventHandler, as its iid says.
type webViewAnswer struct {
	methods *[4]uintptr
	iid     *windows.GUID
	handler *newWindowHandler
}

var (
	newWindowHandlers       []*newWindowHandler
	newWindowAnswerMethods  [4]uintptr
	navigationAnswerMethods [4]uintptr
)

// The answers' methods: QueryInterface, AddRef and Release (they live as
// long as the app), and Invoke. Set at start, as an answer opens new windows
// that are answered in turn.
func init() {
	queryInterface := syscall.NewCallback(func(answer *webViewAnswer, iid *windows.GUID, object *unsafe.Pointer) uintptr {
		if *iid != iidUnknown && *iid != *answer.iid {
			*object = nil
			return comNoInterface
		}
		*object = unsafe.Pointer(answer)
		return 0
	})
	held := syscall.NewCallback(func(*webViewAnswer) uintptr { return 1 })
	newWindowAnswerMethods = [4]uintptr{queryInterface, held, held, syscall.NewCallback(newWindowRequested)}
	navigationAnswerMethods = [4]uintptr{queryInterface, held, held, syscall.NewCallback(navigationStarting)}
}

// listen answers the window's WebView2 unless it already does, on the main
// thread, where Wails makes the WebView2 and WebView2 tells of requests.
func (handler *newWindowHandler) listen() {
	holder, webView := webView2Of(handler.window)
	if webView == nil || holder == handler.holder {
		return
	}
	add := func(place int, answer *webViewAnswer) bool {
		var token int64
		result, _, _ := syscall.SyscallN(webView.method(place),
			uintptr(unsafe.Pointer(webView)), uintptr(unsafe.Pointer(answer)), uintptr(unsafe.Pointer(&token)))
		return result == 0
	}
	if !add(coreWebView2AddNewWindowRequested, &handler.newWindow) {
		return
	}
	add(coreWebView2AddNavigationStarting, &handler.navigation)
	if handler.holder == nil {
		newWindowHandlers = append(newWindowHandlers, handler)
	}
	handler.holder = holder
}

// newWindowRequested answers a request: an address outside the app or one of
// the app's pages opens by the rule and WebView2 opens nothing, a window the
// page fills itself is left to WebView2, and anything else opens nothing.
func newWindowRequested(answer *webViewAnswer, _, args *comObject) uintptr {
	address, ok := argsAddress(args, newWindowArgsGetURI)
	if !ok {
		return 0
	}
	kind, _ := newWindowKindOf(address)
	if kind == newWindowPageMade {
		return 0
	}
	syscall.SyscallN(args.method(newWindowArgsPutHandled), uintptr(unsafe.Pointer(args)), 1)
	if kind != newWindowRefused {
		from := answer.handler.window
		go func() { _ = openNewWindow(address, from) }()
	}
	return 0
}

// navigationStarting answers the page going elsewhere: the app's page taken
// to an address outside the app — by a pressed link without a target, a
// frame's link to the top, or the page's own going there — opens that by the
// rule and the page stays, as Notion's app keeps its window. A redirect and a
// page already outside the app (a sign-in the server sent it to) go on, as
// does going anywhere else.
func navigationStarting(answer *webViewAnswer, sender, args *comObject) uintptr {
	var redirected int32
	syscall.SyscallN(args.method(navigationArgsGetIsRedirected), uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&redirected)))
	if redirected != 0 {
		return 0
	}
	if source, ok := argsAddress(sender, coreWebView2GetSource); !ok || !appAddress(source) {
		return 0
	}
	address, ok := argsAddress(args, navigationArgsGetURI)
	if !ok {
		return 0
	}
	if kind, _ := newWindowKindOf(address); kind != newWindowOutside {
		return 0
	}
	syscall.SyscallN(args.method(navigationArgsPutCancel), uintptr(unsafe.Pointer(args)), 1)
	from := answer.handler.window
	go func() { _ = openNewWindow(address, from) }()
	return 0
}

// appAddress says whether an address is one of the app's own pages.
func appAddress(address string) bool {
	kind, _ := newWindowKindOf(address)
	return kind == newWindowAppPage
}

// argsAddress reads an address a WebView2 object gives at its method place:
// an event's, or the web view's own.
func argsAddress(args *comObject, place int) (string, bool) {
	var uri *uint16
	result, _, _ := syscall.SyscallN(args.method(place), uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&uri)))
	if result != 0 || uri == nil {
		return "", false
	}
	address := windows.UTF16PtrToString(uri)
	windows.CoTaskMemFree(unsafe.Pointer(uri))
	return address, true
}

// webView2Of finds a window's WebView2 and its holder where Wails keeps them
// (wailsChromiumPart, window_tabs_windows.go); nil when the window has none
// yet or Wails keeps it elsewhere.
func webView2Of(window *application.WebviewWindow) (holder unsafe.Pointer, webView *comObject) {
	return wailsChromiumPart(window, "webview", "*edge.ICoreWebView2")
}
