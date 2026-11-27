package main

// desktopProgram is the app's program, as its packages carry it
// (scripts/package-desktop.mjs reads the name here): in the macOS bundle and
// the Windows folder and its MSIX; on Windows with ".exe".
const desktopProgram = "leafen-desktop"

// On Windows the program carries the app's icon as its resource 3: the
// picture with its rounded corners cut clear (build/appicon-rounded.png), as
// Windows draws an icon as it is and cuts no shape of its own. Wails draws it
// in the window's title bar and taskbar button and Explorer shows it for the
// program: Go links it from rsrc_windows_*.syso, which go-winres makes from
// build/windows/winres.json. Run `go generate` here when the icon changes.
//
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64,arm64 --out rsrc
