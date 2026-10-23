//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

void spytifyShowInDock(int show) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp setActivationPolicy:show ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory];
		if (show) {
			[NSApp activateIgnoringOtherApps:YES];
		}
	});
}

@interface SpytifyReopen : NSObject
@end

@implementation SpytifyReopen
- (void)reopen:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply {
	spytifyReopened();
}
@end

static SpytifyReopen *reopenHandler;

// The handler goes in after Cocoa has installed its own at launch.
void spytifyWatchReopen(void) {
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), dispatch_get_main_queue(), ^{
		reopenHandler = [SpytifyReopen new];
		[[NSAppleEventManager sharedAppleEventManager] setEventHandler:reopenHandler
		                                                    andSelector:@selector(reopen:withReply:)
		                                                  forEventClass:kCoreEventClass
		                                                     andEventID:kAEReopenApplication];
	});
}
