//go:build darwin

package tray

/*
#cgo darwin CFLAGS: -DDARWIN -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

@interface NotifMenuDelegate : NSObject <NSMenuDelegate>
@property (strong) NSStatusItem *statusItem;
@property (strong) NSMenu *originalMenu;
@property (strong) NSTimer *timer;
@end

@implementation NotifMenuDelegate
- (void)menuDidClose:(NSMenu *)menu {
    if (self.timer) {
        [self.timer invalidate];
        self.timer = nil;
    }
    if (self.statusItem && self.originalMenu) {
        [self.statusItem setMenu:self.originalMenu];
    }
    self.statusItem = nil;
    self.originalMenu = nil;
}
@end

static NotifMenuDelegate *g_notifDelegate = nil;

void showNotificationPopupDarwin(const char* title, const char* message, double seconds) {
    NSString *nsTitle = title ? [NSString stringWithUTF8String:title] : @"";
    NSString *nsMsg = message ? [NSString stringWithUTF8String:message] : @"";

    dispatch_async(dispatch_get_main_queue(), ^{
        id delegate = [NSApp delegate];
        if (!delegate) return;

        Ivar statusItemIvar = class_getInstanceVariable([delegate class], "statusItem");
        Ivar menuIvar = class_getInstanceVariable([delegate class], "menu");
        if (!statusItemIvar || !menuIvar) return;

        NSStatusItem *statusItem = object_getIvar(delegate, statusItemIvar);
        NSMenu *originalMenu = object_getIvar(delegate, menuIvar);
        if (!statusItem || !originalMenu) return;

        // If a notification popup is currently active, close it first
        if (g_notifDelegate && g_notifDelegate.timer) {
            [statusItem.menu cancelTracking];
        }

        // Create temporary single-notification menu
        NSMenu *notifMenu = [[NSMenu alloc] initWithTitle:@"Notification"];
        [notifMenu setAutoenablesItems:NO];

        NSString *fullText = [NSString stringWithFormat:@"%@: %@", nsTitle, nsMsg];
        NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:fullText action:nil keyEquivalent:@""];
        [item setEnabled:NO];
        [notifMenu addItem:item];

        if (!g_notifDelegate) {
            g_notifDelegate = [[NotifMenuDelegate alloc] init];
        }
        g_notifDelegate.statusItem = statusItem;
        g_notifDelegate.originalMenu = originalMenu;
        [notifMenu setDelegate:g_notifDelegate];

        [statusItem setMenu:notifMenu];

        // Auto dismiss after specified seconds using NSTimer attached to tracking modes
        NSTimer *timer = [NSTimer timerWithTimeInterval:seconds repeats:NO block:^(NSTimer * _Nonnull t) {
            [notifMenu cancelTracking];
        }];
        [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSRunLoopCommonModes];
        [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSEventTrackingRunLoopMode];
        g_notifDelegate.timer = timer;

        if (statusItem.button) {
            [statusItem.button performClick:nil];
        }
    });
}
*/
import "C"
import (
	"time"
	"unsafe"
)

// ShowNotificationPopup opens a single-item notification popup from the tray icon for duration d.
func ShowNotificationPopup(title, message string, d time.Duration) {
	cTitle := C.CString(title)
	cMsg := C.CString(message)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cMsg))

	C.showNotificationPopupDarwin(cTitle, cMsg, C.double(d.Seconds()))
}
