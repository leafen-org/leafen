//go:build ios
// Starts the iOS app. Apps built with the iOS 27 SDK must use UIKit's scene
// life cycle, which Wails' app delegate (in the Go archive) predates: the
// scene delegate here hands the delegate's window to the scene as it
// connects, so Wails keeps building its web view in that window, and passes
// the scene's activity on as the application events Wails listens for.
#import <UIKit/UIKit.h>
#include <stdio.h>

// The Go side keeps a link the app is opened with for its window (links_ios.go).
extern void leafenLinkOpened(char *link);
// Notifications without push (notices_ios.m): taps and the background refresh.
extern void leafenNoticesStart(void);
extern void leafenNoticesSchedule(void);

static void openLinks(NSSet<UIOpenURLContext *> *contexts) {
    for (UIOpenURLContext *context in contexts) {
        NSString *link = context.URL.absoluteString;
        if (link.length > 0) leafenLinkOpened((char *)link.UTF8String);
    }
}

@interface LeafenSceneDelegate : UIResponder <UIWindowSceneDelegate>
@property (strong, nonatomic) UIWindow *window;
@end

@implementation LeafenSceneDelegate
- (void)scene:(UIScene *)scene willConnectToSession:(UISceneSession *)session options:(UISceneConnectionOptions *)connectionOptions {
    if (![scene isKindOfClass:[UIWindowScene class]]) return;
    id<UIApplicationDelegate> delegate = UIApplication.sharedApplication.delegate;
    UIWindow *window = [delegate respondsToSelector:@selector(window)] ? delegate.window : nil;
    if (window == nil) return;
    window.windowScene = (UIWindowScene *)scene;
    self.window = window;
    [window makeKeyAndVisible];
    // A leafen: link the app was started with.
    openLinks(connectionOptions.URLContexts);
}

// A leafen: link opened while the app runs.
- (void)scene:(UIScene *)scene openURLContexts:(NSSet<UIOpenURLContext *> *)URLContexts {
    openLinks(URLContexts);
}

- (void)forward:(SEL)selector {
    id delegate = UIApplication.sharedApplication.delegate;
    if ([delegate respondsToSelector:selector]) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Warc-performSelector-leaks"
        [delegate performSelector:selector withObject:UIApplication.sharedApplication];
#pragma clang diagnostic pop
    }
}

- (void)sceneDidBecomeActive:(UIScene *)scene { [self forward:@selector(applicationDidBecomeActive:)]; }
- (void)sceneWillResignActive:(UIScene *)scene { [self forward:@selector(applicationWillResignActive:)]; }
- (void)sceneWillEnterForeground:(UIScene *)scene { [self forward:@selector(applicationWillEnterForeground:)]; }
- (void)sceneDidEnterBackground:(UIScene *)scene {
    // The next background refresh asks for what comes while the app is away.
    leafenNoticesSchedule();
    [self forward:@selector(applicationDidEnterBackground:)];
}
@end

int main(int argc, char * argv[]) {
    @autoreleasepool {
        // Unbuffered, so the Go side's logs flush at once.
        setvbuf(stdout, NULL, _IONBF, 0);
        setvbuf(stderr, NULL, _IONBF, 0);
        // Notification taps and the background refresh must be in place
        // before the launch ends; nothing else starts here: Wails' delegate
        // starts the Go runtime once UIKit has delivered the launch.
        leafenNoticesStart();
        return UIApplicationMain(argc, argv, nil, @"WailsAppDelegate");
    }
}
