//go:build ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework Foundation
#include <stdlib.h>
#import <UIKit/UIKit.h>

// Offers the saved file through the share sheet: "Save to Files" and the
// apps that take it.
static void leafenShareFile(const char *path) {
	NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
	dispatch_async(dispatch_get_main_queue(), ^{
		UIWindow *window = nil;
		for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
			if (![scene isKindOfClass:[UIWindowScene class]]) continue;
			for (UIWindow *candidate in ((UIWindowScene *)scene).windows) {
				if (candidate.isKeyWindow || window == nil) window = candidate;
			}
		}
		UIViewController *root = window.rootViewController;
		while (root.presentedViewController) root = root.presentedViewController;
		if (root == nil) return;
		UIActivityViewController *share = [[UIActivityViewController alloc] initWithActivityItems:@[url] applicationActivities:nil];
		share.popoverPresentationController.sourceView = root.view;
		share.popoverPresentationController.sourceRect = CGRectMake(CGRectGetMidX(root.view.bounds), CGRectGetMidY(root.view.bounds), 0, 0);
		[root presentViewController:share animated:YES completion:nil];
	});
}
*/
import "C"

import (
	"os"
	"path/filepath"
	"unsafe"
)

// chooseSavePath makes the file in the app's own place for files on their
// way to the share sheet, emptied of earlier ones.
func chooseSavePath(name string) (string, error) {
	directory := filepath.Join(os.TempDir(), "leafen-saved")
	_ = os.RemoveAll(directory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

// finishSave offers the file through the share sheet.
func finishSave(path string) error {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	C.leafenShareFile(value)
	return nil
}
