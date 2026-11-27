//go:build windows

package commandline

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// A command typed in a terminal on Windows. The desktop app's program is a
// window program, which Windows starts with no console: in PowerShell or cmd
// a command said nothing at all (2026-11-19, `setup status`). It now writes
// to the console of the terminal it was typed in, as Unicode (Go writes a
// console file with WriteConsoleW), so any language shows as typed. Typed as
// `leafen-desktop`, the console program beside it (desktop/console) runs it,
// so the terminal also waits and takes its exit code; typed with .exe, the
// terminal doesn't wait and the lines come after its prompt. The server's
// program (cmd/leafen, leafen.exe) is a console program, which has the
// terminal's console from its start. Output either one writes elsewhere
// than the console, which PowerShell reads, goes in the console's code page
// (codepage_windows.go).

var (
	consoleKernel32   = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = consoleKernel32.NewProc("AttachConsole")
	// The files a console file or a pipe took the place of: a dropped
	// os.File closes its handle when it is collected.
	replacedStdFiles []*os.File
)

// Prepare sets the input and output of the command line args up as above;
// what it returns finishes writing them before the program ends. A command
// whose streams carry a protocol (speaksProtocol) keeps them in UTF-8.
func Prepare(args []string) func() {
	streams := []*os.File{os.Stdin, os.Stdout, os.Stderr}
	needsConsole := false
	for _, stream := range streams {
		kind := standardHandleKind(stream)
		needsConsole = needsConsole || kind == windows.FILE_TYPE_UNKNOWN || kind == windows.FILE_TYPE_CHAR
	}
	powerShell := terminalIsPowerShell()
	if !needsConsole && !powerShell {
		return func() {}
	}
	// ATTACH_PARENT_PROCESS: the terminal's console, or the console
	// program's that runs this one; refused where there is none, and where
	// the program has its console already (a console program), whose
	// streams are the console's as they are.
	attached, _, err := procAttachConsole.Call(uintptr(^uint32(0)))
	if attached == 0 && err != windows.ERROR_ACCESS_DENIED {
		return func() {}
	}
	if attached != 0 {
		for index, stream := range streams {
			if replacement := consoleStream(stream, index == 0); replacement != nil {
				replacedStdFiles = append(replacedStdFiles, stream)
				streams[index] = replacement
			}
		}
	}
	var finishes []func()
	if !speaksProtocol(args) {
		finishes = inConsoleCodePage(streams, powerShell)
	}
	os.Stdin, os.Stdout, os.Stderr = streams[0], streams[1], streams[2]
	log.SetOutput(os.Stderr)
	return func() {
		for _, finish := range finishes {
			finish()
		}
	}
}

// standardHandleKind is what a standard stream is: unknown where there is
// none, char for a console (or the NUL device), a pipe or a file.
func standardHandleKind(stream *os.File) uint32 {
	if stream == nil {
		return windows.FILE_TYPE_UNKNOWN
	}
	handle := windows.Handle(stream.Fd())
	if handle == 0 || handle == windows.InvalidHandle {
		return windows.FILE_TYPE_UNKNOWN
	}
	kind, err := windows.GetFileType(handle)
	if err != nil {
		return windows.FILE_TYPE_UNKNOWN
	}
	return kind
}

// consoleStream is the console file for a standard stream that has none
// (the terminal gave a window program none) or that holds the console's
// handle (the console program passed its own), which Go took as a plain
// file before the console was attached; nil for any other.
func consoleStream(stream *os.File, input bool) *os.File {
	switch standardHandleKind(stream) {
	case windows.FILE_TYPE_UNKNOWN:
		name := "CONOUT$"
		if input {
			name = "CONIN$"
		}
		file, err := os.OpenFile(name, os.O_RDWR, 0)
		if err != nil {
			return nil
		}
		return file
	case windows.FILE_TYPE_CHAR:
		var mode uint32
		if windows.GetConsoleMode(windows.Handle(stream.Fd()), &mode) != nil {
			return nil
		}
		return os.NewFile(stream.Fd(), stream.Name())
	}
	return nil
}
