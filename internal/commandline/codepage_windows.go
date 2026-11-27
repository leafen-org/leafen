//go:build windows

package commandline

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Output read by PowerShell rather than shown — piped, captured or sent to a
// file — PowerShell decodes in the console's code page, 949 on a Korean
// Windows, and Korean came out garbled, the name in the confirmation command
// with it (2026-11-19). When the terminal is PowerShell, such output goes in
// that code page, from either program (console_windows.go); any other reader
// (bash, an agent's own pipes) gets UTF-8, as before, and so does a command
// that speaks a protocol (speaksProtocol). Shown in a console, it goes as
// Unicode.

var procWideCharToMultiByte = consoleKernel32.NewProc("WideCharToMultiByte")

// inConsoleCodePage puts the output streams that go elsewhere than a console
// through the console's code page, where PowerShell reads them and the code
// page isn't UTF-8; what it returns waits for the last of each.
func inConsoleCodePage(streams []*os.File, powerShell bool) []func() {
	codePage, err := windows.GetConsoleOutputCP()
	if err != nil || !powerShell || codePage == 0 || codePage == 65001 {
		return nil
	}
	finishes := []func(){}
	for index := 1; index < len(streams); index++ {
		if kind := standardHandleKind(streams[index]); kind == windows.FILE_TYPE_PIPE || kind == windows.FILE_TYPE_DISK {
			if replacement, finish := codePageStream(streams[index], codePage); replacement != nil {
				streams[index] = replacement
				finishes = append(finishes, finish)
			}
		}
	}
	return finishes
}

// codePageStream is a pipe whose UTF-8 goes on to stream in the code page,
// and what waits for the last of it.
func codePageStream(stream *os.File, codePage uint32) (*os.File, func()) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32<<10)
		pending := []byte{}
		for {
			count, err := reader.Read(buffer)
			pending = append(pending, buffer[:count]...)
			// A character cut between two reads waits for its rest.
			whole := len(pending)
			for at := len(pending) - 1; at >= 0 && at >= len(pending)-utf8.UTFMax; at-- {
				if utf8.RuneStart(pending[at]) {
					if !utf8.FullRune(pending[at:]) {
						whole = at
					}
					break
				}
			}
			if err != nil {
				whole = len(pending)
			}
			if whole > 0 {
				_, _ = stream.Write(inCodePage(pending[:whole], codePage))
				pending = append([]byte{}, pending[whole:]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return writer, func() {
		_ = writer.Close()
		<-done
	}
}

// inCodePage is UTF-8 text in the code page: a character it lacks as its
// plain stand-in where it has one (codePageStandIns), else as the code page's
// default character, '?'; the text as it was where Windows can't convert it.
func inCodePage(text []byte, codePage uint32) []byte {
	out, lacking, ok := toCodePage(string(text), codePage)
	if !ok {
		return text
	}
	if !lacking {
		return out
	}
	var plain strings.Builder
	for _, char := range string(text) {
		if standIn, has := codePageStandIns[char]; has {
			if _, lacks, _ := toCodePage(string(char), codePage); lacks {
				plain.WriteString(standIn)
				continue
			}
		}
		plain.WriteRune(char)
	}
	if out, _, ok = toCodePage(plain.String(), codePage); !ok {
		return text
	}
	return out
}

// codePageStandIns stand in for the punctuation Leafen's words use where a
// code page lacks it: 949, a Korean Windows', has no — or ›, and a question's
// "— the person who sets it up" or "the sidebar › Add a device" read with a
// '?' (2026-11-19). The words keep them: a console shows them as written
// (console_windows.go), as do UTF-8 terminals, an agent's own pipes,
// MCP and the screens; only text put in such a code page takes these.
var codePageStandIns = map[rune]string{
	'—': "-", '–': "-", '‒': "-", '―': "-", '‐': "-", '‑': "-", '−': "-",
	'›': ">", '‹': "<", '»': ">>", '«': "<<", '→': "->", '←': "<-",
	'…': "...", '“': `"`, '”': `"`, '„': `"`, '‘': "'", '’': "'", '‚': "'",
	'·': "-", '•': "*", '≥': ">=", '≤': "<=", '×': "x",
	'\u00a0': " ", '\u2009': " ", '\u202f': " ",
}

// toCodePage is text in the code page, and whether a character in it went to
// the code page's default character; not ok where Windows can't convert it.
func toCodePage(text string, codePage uint32) (out []byte, lacking, ok bool) {
	wide := utf16.Encode([]rune(text))
	if len(wide) == 0 {
		return []byte{}, false, true
	}
	var usedDefault int32
	if out = wideToCodePage(wide, codePage, &usedDefault); out != nil {
		return out, usedDefault != 0, true
	}
	// A code page that can't say so (UTF-7, GB18030 …) converts without.
	if out = wideToCodePage(wide, codePage, nil); out != nil {
		return out, false, true
	}
	return nil, false, false
}

// wideToCodePage is UTF-16 text in the code page, nil where Windows refuses;
// usedDefault, where given, says whether a character took the default.
func wideToCodePage(wide []uint16, codePage uint32, usedDefault *int32) []byte {
	used := uintptr(0)
	if usedDefault != nil {
		used = uintptr(unsafe.Pointer(usedDefault))
	}
	size, _, _ := procWideCharToMultiByte.Call(uintptr(codePage), 0, uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)), 0, 0, 0, used)
	if size == 0 {
		return nil
	}
	if usedDefault != nil {
		*usedDefault = 0
	}
	out := make([]byte, size)
	written, _, _ := procWideCharToMultiByte.Call(uintptr(codePage), 0, uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)),
		uintptr(unsafe.Pointer(&out[0])), size, 0, used)
	if written == 0 {
		return nil
	}
	return out[:written]
}

// terminalIsPowerShell says whether the command was typed in PowerShell:
// its parent, or the parent of the desktop app's console program
// (leafen-desktop.com) where that one runs it.
func terminalIsPowerShell() bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	parents, names := map[uint32]uint32{}, map[uint32]string{}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
		names[entry.ProcessID] = strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
	}
	parent := parents[uint32(os.Getpid())]
	if strings.EqualFold(filepath.Ext(names[parent]), ".com") {
		parent = parents[parent]
	}
	return names[parent] == "powershell.exe" || names[parent] == "pwsh.exe"
}
