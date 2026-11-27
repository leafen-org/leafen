//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// leafenAppIcon is the app's icon as PNG bytes the caller frees, or NULL.
static void *leafenAppIcon(int *length) {
	*length = 0;
	CGImageRef image = [NSApp.applicationIconImage CGImageForProposedRect:NULL context:nil hints:nil];
	if (!image) return NULL;
	NSData *png = [[[NSBitmapImageRep alloc] initWithCGImage:image] representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
	if (!png.length) return NULL;
	void *bytes = malloc(png.length);
	memcpy(bytes, png.bytes, png.length);
	*length = (int)png.length;
	return bytes;
}
*/
import "C"

import "unsafe"

// appIcon is the app's own icon as a PNG, which Notion's app shows on its
// message boxes where Wails' info dialog would show an "i". It runs on the
// main thread.
func appIcon() []byte {
	var length C.int
	bytes := C.leafenAppIcon(&length)
	if bytes == nil {
		return nil
	}
	defer C.free(bytes)
	return C.GoBytes(unsafe.Pointer(bytes), length)
}
