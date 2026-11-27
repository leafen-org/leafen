//go:build ios

#import <AVFoundation/AVFoundation.h>
#import <UIKit/UIKit.h>

extern void leafenCodeScanned(char *outcome, char *text);

// The camera full screen with a close button, until it reads a QR code.
@interface LeafenCodeScanner : UIViewController <AVCaptureMetadataOutputObjectsDelegate>
@property(nonatomic, strong) AVCaptureSession *session;
@property(nonatomic, strong) AVCaptureVideoPreviewLayer *preview;
@property(nonatomic, assign) BOOL answered;
@end

@implementation LeafenCodeScanner
- (void)viewDidLoad {
	[super viewDidLoad];
	self.view.backgroundColor = UIColor.blackColor;
	self.overrideUserInterfaceStyle = UIUserInterfaceStyleDark;
	self.preview = [AVCaptureVideoPreviewLayer layerWithSession:self.session];
	self.preview.videoGravity = AVLayerVideoGravityResizeAspectFill;
	[self.view.layer addSublayer:self.preview];
	UIButton *close = [UIButton buttonWithType:UIButtonTypeClose];
	close.translatesAutoresizingMaskIntoConstraints = NO;
	[close addTarget:self action:@selector(close) forControlEvents:UIControlEventTouchUpInside];
	[self.view addSubview:close];
	[NSLayoutConstraint activateConstraints:@[
		[close.topAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.topAnchor constant:12],
		[close.leadingAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.leadingAnchor constant:16],
	]];
	AVCaptureSession *session = self.session;
	dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{ [session startRunning]; });
}

- (void)viewDidLayoutSubviews {
	[super viewDidLayoutSubviews];
	self.preview.frame = self.view.bounds;
}

- (void)metadataOutput:(AVCaptureMetadataOutput *)output didOutputMetadataObjects:(NSArray<__kindof AVMetadataObject *> *)objects fromConnection:(AVCaptureConnection *)connection {
	for (AVMetadataObject *object in objects) {
		if (![object isKindOfClass:[AVMetadataMachineReadableCodeObject class]]) continue;
		NSString *text = ((AVMetadataMachineReadableCodeObject *)object).stringValue;
		if (text.length > 0) {
			[self answer:@"scanned" text:text];
			return;
		}
	}
}

- (void)close {
	[self answer:@"cancelled" text:@""];
}

- (void)answer:(NSString *)outcome text:(NSString *)text {
	if (self.answered) return;
	self.answered = YES;
	AVCaptureSession *session = self.session;
	dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{ [session stopRunning]; });
	leafenCodeScanned((char *)outcome.UTF8String, (char *)text.UTF8String);
	[self dismissViewControllerAnimated:YES completion:nil];
}
@end

static UIViewController *leafenTopController(void) {
	UIWindow *window = nil;
	for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
		if (![scene isKindOfClass:[UIWindowScene class]]) continue;
		for (UIWindow *candidate in ((UIWindowScene *)scene).windows) {
			if (candidate.isKeyWindow || window == nil) window = candidate;
		}
	}
	UIViewController *top = window.rootViewController;
	while (top.presentedViewController) top = top.presentedViewController;
	return top;
}

// A camera reading QR codes, or nil when the device has none that can.
static AVCaptureSession *leafenCodeSession(id<AVCaptureMetadataOutputObjectsDelegate> delegate) {
	AVCaptureDevice *camera = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
	if (camera == nil) return nil;
	AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:camera error:nil];
	AVCaptureSession *session = [AVCaptureSession new];
	AVCaptureMetadataOutput *output = [AVCaptureMetadataOutput new];
	if (input == nil || ![session canAddInput:input] || ![session canAddOutput:output]) return nil;
	[session addInput:input];
	[session addOutput:output];
	if (![output.availableMetadataObjectTypes containsObject:AVMetadataObjectTypeQRCode]) return nil;
	output.metadataObjectTypes = @[AVMetadataObjectTypeQRCode];
	[output setMetadataObjectsDelegate:delegate queue:dispatch_get_main_queue()];
	return session;
}

static void leafenShowCodeScanner(void) {
	LeafenCodeScanner *scanner = [LeafenCodeScanner new];
	scanner.session = leafenCodeSession(scanner);
	UIViewController *top = leafenTopController();
	if (scanner.session == nil || top == nil) {
		leafenCodeScanned("unavailable", "");
		return;
	}
	scanner.modalPresentationStyle = UIModalPresentationFullScreen;
	[top presentViewController:scanner animated:YES completion:nil];
}

// Asks for the camera only now, when the person chose to scan; a device
// with no camera (a simulator) is never asked.
void leafenStartCodeScan(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if ([AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo] == nil) {
			leafenCodeScanned("unavailable", "");
			return;
		}
		switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo]) {
		case AVAuthorizationStatusAuthorized:
			leafenShowCodeScanner();
			break;
		case AVAuthorizationStatusNotDetermined:
			[AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL granted) {
				dispatch_async(dispatch_get_main_queue(), ^{
					if (granted) leafenShowCodeScanner();
					else leafenCodeScanned("denied", "");
				});
			}];
			break;
		default:
			leafenCodeScanned("denied", "");
			break;
		}
	});
}
