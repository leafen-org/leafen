//go:build ios

#import <BackgroundTasks/BackgroundTasks.h>
#import <UIKit/UIKit.h>
#import <UserNotifications/UserNotifications.h>

// A phone's notifications without push (notices_ios.go): iOS wakes the app
// for a background refresh now and then, the Go side asks the server and
// shows what came here; a tap opens the page through the app's links. A
// question card's or an approval's notice has its buttons and, for a card of
// one question, a reply; pressing one hands it to the Go side, which answers
// without bringing the app forward.

extern void leafenLinkOpened(char *link);
extern void leafenRefreshNotices(void);
extern void leafenNoticeAnswer(char *kept, char *action, char *text);

static NSString *const leafenNoticesTask = @"org.leafen.app.notices";

// Each notice's buttons are a category of its own, the newest few kept so
// their buttons stay when the app starts again.
static NSString *const leafenNoticeCategoriesKey = @"leafen.noticeCategories";
static const NSUInteger leafenNoticeCategoriesKept = 16;

static NSString *leafenText(id value) {
	return [value isKindOfClass:[NSString class]] ? value : @"";
}

static UNNotificationCategory *leafenNoticeCategory(NSDictionary *kept) {
	NSMutableArray *actions = [NSMutableArray array];
	NSArray *buttons = [kept[@"actions"] isKindOfClass:[NSArray class]] ? kept[@"actions"] : @[];
	for (NSDictionary *button in buttons) {
		if (![button isKindOfClass:[NSDictionary class]]) continue;
		NSString *action = leafenText(button[@"action"]), *title = leafenText(button[@"title"]);
		if (action.length == 0 || title.length == 0) continue;
		[actions addObject:[UNNotificationAction actionWithIdentifier:action title:title options:UNNotificationActionOptionNone]];
	}
	NSString *reply = leafenText(kept[@"reply"]), *send = leafenText(kept[@"send"]);
	if (reply.length > 0) {
		[actions addObject:[UNTextInputNotificationAction actionWithIdentifier:@"reply" title:reply options:UNNotificationActionOptionNone
		                                                  textInputButtonTitle:(send.length > 0 ? send : reply) textInputPlaceholder:@""]];
	}
	return [UNNotificationCategory categoryWithIdentifier:leafenText(kept[@"id"]) actions:actions intentIdentifiers:@[]
	                                              options:UNNotificationCategoryOptionNone];
}

static void leafenNoticeCategoriesApply(NSArray *kept) {
	NSMutableSet *categories = [NSMutableSet set];
	for (NSDictionary *one in kept) {
		if ([one isKindOfClass:[NSDictionary class]] && leafenText(one[@"id"]).length > 0) [categories addObject:leafenNoticeCategory(one)];
	}
	[[UNUserNotificationCenter currentNotificationCenter] setNotificationCategories:categories];
}

// leafenNoticeCategoryFor keeps a notice's buttons as its category and names it.
static NSString *leafenNoticeCategoryFor(NSString *identifier, NSDictionary *notice) {
	NSDictionary *answer = [notice[@"answer"] isKindOfClass:[NSDictionary class]] ? notice[@"answer"] : @{};
	NSString *category = [@"leafen.notice." stringByAppendingString:identifier];
	NSMutableArray *buttons = [NSMutableArray array];
	for (NSDictionary *button in ([notice[@"actions"] isKindOfClass:[NSArray class]] ? notice[@"actions"] : @[])) {
		if ([button isKindOfClass:[NSDictionary class]]) [buttons addObject:@{@"action": leafenText(button[@"action"]), @"title": leafenText(button[@"title"])}];
	}
	NSDictionary *entry = @{@"id": category, @"actions": buttons, @"reply": leafenText(answer[@"reply"]), @"send": leafenText(answer[@"send"])};
	@synchronized (leafenNoticeCategoriesKey) {
		NSUserDefaults *defaults = [NSUserDefaults standardUserDefaults];
		NSMutableArray *kept = [NSMutableArray array];
		for (NSDictionary *one in ([defaults arrayForKey:leafenNoticeCategoriesKey] ?: @[])) {
			if ([one isKindOfClass:[NSDictionary class]] && ![leafenText(one[@"id"]) isEqualToString:category]) [kept addObject:one];
		}
		[kept addObject:entry];
		while (kept.count > leafenNoticeCategoriesKept) [kept removeObjectAtIndex:0];
		[defaults setObject:kept forKey:leafenNoticeCategoriesKey];
		leafenNoticeCategoriesApply(kept);
	}
	return category;
}

@interface LeafenNoticeDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation LeafenNoticeDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
    didReceiveNotificationResponse:(UNNotificationResponse *)response
             withCompletionHandler:(void (^)(void))completionHandler {
	NSDictionary *info = response.notification.request.content.userInfo;
	NSString *action = response.actionIdentifier, *kept = leafenText(info[@"notice"]);
	if (kept.length > 0 && ![action isEqualToString:UNNotificationDefaultActionIdentifier] &&
	    ![action isEqualToString:UNNotificationDismissActionIdentifier]) {
		NSString *text = [response isKindOfClass:[UNTextInputNotificationResponse class]]
			? leafenText(((UNTextInputNotificationResponse *)response).userText) : @"";
		// The answer goes in the time iOS gives; the handler ends it.
		dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
			leafenNoticeAnswer((char *)kept.UTF8String, (char *)action.UTF8String, (char *)text.UTF8String);
			completionHandler();
		});
		return;
	}
	NSString *link = info[@"link"];
	if ([link isKindOfClass:[NSString class]] && link.length > 0) leafenLinkOpened((char *)link.UTF8String);
	completionHandler();
}

// While the app is in front, its own inbox shows what came.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
	completionHandler(UNNotificationPresentationOptionNone);
}
@end

static LeafenNoticeDelegate *leafenNoticeDelegate;

// leafenNoticesSchedule asks iOS for the next background refresh, no sooner
// than fifteen minutes from now; iOS picks the moment.
void leafenNoticesSchedule(void) {
	BGAppRefreshTaskRequest *request = [[BGAppRefreshTaskRequest alloc] initWithIdentifier:leafenNoticesTask];
	request.earliestBeginDate = [NSDate dateWithTimeIntervalSinceNow:15 * 60];
	[[BGTaskScheduler sharedScheduler] submitTaskRequest:request error:nil];
}

// leafenNoticesStart takes notification taps and registers the background
// refresh; both must be in place before the app finishes launching, so
// build/ios/main.m calls it first. Wails' app delegate puts a notification
// delegate of its own in place as the launch finishes (ios_notifications_init),
// which took the taps and buttons from this one (seen 2026-10-24); this one
// takes them back right after, still within the launch.
void leafenNoticesStart(void) {
	leafenNoticeDelegate = [LeafenNoticeDelegate new];
	[UNUserNotificationCenter currentNotificationCenter].delegate = leafenNoticeDelegate;
	[[NSNotificationCenter defaultCenter] addObserverForName:UIApplicationDidFinishLaunchingNotification object:nil queue:nil
	                                              usingBlock:^(NSNotification *launched) {
		[UNUserNotificationCenter currentNotificationCenter].delegate = leafenNoticeDelegate;
	}];
	leafenNoticeCategoriesApply([[NSUserDefaults standardUserDefaults] arrayForKey:leafenNoticeCategoriesKey] ?: @[]);
	[[BGTaskScheduler sharedScheduler] registerForTaskWithIdentifier:leafenNoticesTask usingQueue:nil
	                                                   launchHandler:^(__kindof BGTask *task) {
		leafenNoticesSchedule();
		// The task ends once: when the refresh is done, or when iOS calls time.
		NSObject *lock = [NSObject new];
		__block BOOL finished = NO;
		void (^finish)(BOOL) = ^(BOOL success) {
			@synchronized (lock) {
				if (finished) return;
				finished = YES;
			}
			[task setTaskCompletedWithSuccess:success];
		};
		task.expirationHandler = ^{ finish(NO); };
		dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
			leafenRefreshNotices();
			finish(YES);
		});
	}];
}

// leafenNoticesAllowed says whether iOS lets the app show notifications and
// badges, asking the person first when ask is set and they haven't answered.
int leafenNoticesAllowed(int ask) {
	UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	__block int allowed = 0;
	[center getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
		if (settings.authorizationStatus == UNAuthorizationStatusAuthorized ||
		    settings.authorizationStatus == UNAuthorizationStatusProvisional) {
			allowed = 1;
			dispatch_semaphore_signal(done);
		} else if (ask && settings.authorizationStatus == UNAuthorizationStatusNotDetermined) {
			[center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionBadge | UNAuthorizationOptionSound)
			                      completionHandler:^(BOOL granted, NSError *error) {
				allowed = granted ? 1 : 0;
				dispatch_semaphore_signal(done);
			}];
		} else {
			dispatch_semaphore_signal(done);
		}
	}];
	dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 120 * NSEC_PER_SEC));
	return allowed;
}

// leafenNoticeShow shows one notification now, the link in it for a tap;
// buttons, when not empty, is the notice as JSON with its buttons, kept in it
// for the answer. The same identifier takes the place of the one shown.
void leafenNoticeShow(const char *identifier, const char *title, const char *body, const char *link, const char *buttons) {
	NSString *name = [NSString stringWithUTF8String:identifier];
	NSString *kept = [NSString stringWithUTF8String:buttons];
	UNMutableNotificationContent *content = [UNMutableNotificationContent new];
	content.title = [NSString stringWithUTF8String:title];
	content.body = [NSString stringWithUTF8String:body];
	content.sound = [UNNotificationSound defaultSound];
	NSMutableDictionary *info = [@{@"link": [NSString stringWithUTF8String:link]} mutableCopy];
	NSDictionary *notice = kept.length > 0
		? [NSJSONSerialization JSONObjectWithData:[kept dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil] : nil;
	if ([notice isKindOfClass:[NSDictionary class]]) {
		content.categoryIdentifier = leafenNoticeCategoryFor(name, notice);
		info[@"notice"] = kept;
	}
	content.userInfo = info;
	UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:name content:content trigger:nil];
	[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:request withCompletionHandler:nil];
}

// leafenNoticeBadge sets the count on the app's icon.
void leafenNoticeBadge(int count) {
	if (@available(iOS 16.0, *)) {
		[[UNUserNotificationCenter currentNotificationCenter] setBadgeCount:count withCompletionHandler:nil];
	} else {
		dispatch_async(dispatch_get_main_queue(), ^{
			[UIApplication sharedApplication].applicationIconBadgeNumber = count;
		});
	}
}
