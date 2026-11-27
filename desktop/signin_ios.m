//go:build ios

#import <AuthenticationServices/AuthenticationServices.h>
#import <UIKit/UIKit.h>

extern void leafenSignInReturned(char *identifier, char *address);

// The sheet shows over the app's key window.
@interface LeafenSignInAnchor : NSObject <ASWebAuthenticationPresentationContextProviding>
@end

@implementation LeafenSignInAnchor
- (ASPresentationAnchor)presentationAnchorForWebAuthenticationSession:(ASWebAuthenticationSession *)session {
	UIWindow *window = nil;
	for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
		if (![scene isKindOfClass:[UIWindowScene class]]) continue;
		for (UIWindow *candidate in ((UIWindowScene *)scene).windows) {
			if (candidate.isKeyWindow || window == nil) window = candidate;
		}
	}
	return window;
}
@end

static LeafenSignInAnchor *leafenSignInAnchor;
static ASWebAuthenticationSession *leafenSignInSession;

// The sheet shows only over an active app: a sign-in a link asked for, as
// the link brings the app forward, starts once the app is active
// (2026-10-21: it started too soon and ended without showing).
static void leafenWhenActive(void (^then)(void)) {
	if ([UIApplication sharedApplication].applicationState == UIApplicationStateActive) {
		then();
		return;
	}
	__block id observer = [[NSNotificationCenter defaultCenter] addObserverForName:UIApplicationDidBecomeActiveNotification
		object:nil queue:[NSOperationQueue mainQueue] usingBlock:^(NSNotification *note) {
			[[NSNotificationCenter defaultCenter] removeObserver:observer];
			then();
		}];
}

// Shows a server's sign-in in the system's sign-in sheet, which ends at the
// app's scheme; what it ended at, or nothing, goes back to Go.
void leafenStartSignIn(const char *identifier, const char *address, const char *scheme) {
	NSString *ident = [NSString stringWithUTF8String:identifier];
	NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
	NSString *callbackScheme = [NSString stringWithUTF8String:scheme];
	dispatch_async(dispatch_get_main_queue(), ^{
		leafenWhenActive(^{
			if (leafenSignInAnchor == nil) leafenSignInAnchor = [LeafenSignInAnchor new];
			[leafenSignInSession cancel];
			leafenSignInSession = [[ASWebAuthenticationSession alloc] initWithURL:url
				callbackURLScheme:callbackScheme
				completionHandler:^(NSURL *callback, NSError *error) {
					NSString *returned = callback != nil ? callback.absoluteString : @"";
					leafenSignInReturned((char *)ident.UTF8String, (char *)returned.UTF8String);
				}];
			leafenSignInSession.presentationContextProvider = leafenSignInAnchor;
			if (![leafenSignInSession start]) leafenSignInReturned((char *)ident.UTF8String, "");
		});
	});
}
