// notifier.m — what macOS is asked to do, and nothing else.
//
// Everything above this file decides whether there is anything to say. This one
// says it: it owns the notification centre delegate, the permission, and the
// drawing of the icon every banner carries.

#import <Cocoa/Cocoa.h>
#import <UserNotifications/UserNotifications.h>
#import "notifier.h"

// goBannerWasTapped is in Go: tapping a banner focuses that session, which is
// thing every display in this system does with a click.
extern void goBannerWasTapped(char *key);

static id watcher = nil;

// The mark on the icon: the invader, 11 across and 9 down.
//
// It is the same sprite the menu bar display wears, written out again rather
// than shared, because these two programs are separate repositories on purpose
// (D-37) and nine lines of ASCII is a cheaper coupling than a module.
//
// Read off the picture it came from rather than typed from memory. There are a
// dozen arrangements of this sprite about and they differ in the antennae and
// the legs, which are exactly the parts anybody looks at.
#define invaderColumns 11
#define invaderRows 9
static const char *invader[invaderRows] = {
    "..X.....X..",
    "X..X...X..X",
    "X.XXXXXXX.X",
    "XXXXXXXXXXX",
    "XXX.XXX.XXX",
    "XXXXXXXXXXX",
    ".XXXXXXXXX.",
    "..X.....X..",
    ".X.......X.",
};


// hex is one Rosé Pine value, written the way the palette writes it.
static NSColor *hex(uint32_t rgb) {
    return [NSColor colorWithSRGBRed:((rgb >> 16) & 0xff) / 255.0
                               green:((rgb >> 8) & 0xff) / 255.0
                                blue:(rgb & 0xff) / 255.0
                               alpha:1.0];
}

// ── the app icon ─────────────────────────────────────────────────────────────

// drawInvaderPNG renders one: a rounded plum tile with the invader on it, in whatever
// colour it is given.
//
// It is the app icon and it is also the picture on a banner, which is why the
// colour is an argument. The icon is drawn in Iris — the program, not a state —
// and a banner's is drawn in the colour of the state it is about, so that a
// notification and the bar are saying the same thing in the same language.
//
// The corner radius is 0.2237 of the width, which is the ratio macOS uses, and
// the 5.5% inset is the margin every app icon leaves so that its corners are
// not flush with the next icon's in the Dock.
static BOOL drawInvaderPNG(CGFloat size, uint32_t rgb, NSString *path) {
    NSBitmapImageRep *sheet = [[NSBitmapImageRep alloc]
        initWithBitmapDataPlanes:NULL
                      pixelsWide:(NSInteger)size
                      pixelsHigh:(NSInteger)size
                   bitsPerSample:8
                 samplesPerPixel:4
                        hasAlpha:YES
                        isPlanar:NO
                  colorSpaceName:NSCalibratedRGBColorSpace
                     bytesPerRow:0
                    bitsPerPixel:0];
    if (sheet == nil) return NO;

    [NSGraphicsContext saveGraphicsState];
    [NSGraphicsContext setCurrentContext:
        [NSGraphicsContext graphicsContextWithBitmapImageRep:sheet]];

    CGFloat margin = size * 0.055;
    NSRect body = NSMakeRect(margin, margin, size - 2 * margin, size - 2 * margin);
    NSBezierPath *rounded = [NSBezierPath bezierPathWithRoundedRect:body
                                                            xRadius:body.size.width * 0.2237
                                                            yRadius:body.size.width * 0.2237];
    // Rosé Pine, the same palette the item and the menu are drawn in, so that
    // the icon in a Notification Centre and the marks on the bar are recognisably
    // one program. The canvas is the theme's own night — Overlay down to Base —
    // and NOT graphite: a grey tile is indistinguishable from the dozen other
    // grey tiles in a column of banners, and the whole job of an app icon is to
    // be picked out of one without being read.
    NSGradient *night = [[NSGradient alloc]
        initWithStartingColor:hex(0x26233a)   // Overlay
                  endingColor:hex(0x191724)]; // Base
    [night drawInBezierPath:rounded angle:-90.0];

    [hex(rgb) set];

    // Whole pixels, always. A sprite is a grid of squares and the one thing it
    // cannot survive is being drawn on fractional boundaries — half-lit edges at
    // sixteen points read as a mistake rather than as pixel art — so the cell is
    // rounded to an integer before anything is placed, and never goes below one.
    CGFloat cell = roundf(size * 0.0545);
    if (cell < 1.0) cell = 1.0;
    CGFloat across = cell * invaderColumns, down = cell * invaderRows;
    CGFloat left = roundf((size - across) / 2.0), bottom = roundf((size - down) / 2.0);

    for (int row = 0; row < invaderRows; row++) {
        for (int column = 0; column < invaderColumns; column++) {
            if (invader[row][column] != 'X') continue;
            // The sprite is written top-down and this context counts up from
            // the bottom.
            NSRectFill(NSMakeRect(left + column * cell,
                                  bottom + (invaderRows - 1 - row) * cell,
                                  cell, cell));
        }
    }

    [NSGraphicsContext restoreGraphicsState];

    // Written in sRGB, converted from the calibrated space AppKit draws in.
    // Without this the file holds the values of Apple's Generic RGB profile —
    // `#c4a7e7` lands on disk as `#b693e1` — which displays correctly on a
    // colour-managed Mac and is wrong to everything that reads the pixels,
    // including the test that checks this icon is drawn in the palette.
    NSBitmapImageRep *tagged = (NSBitmapImageRep *)[sheet
        bitmapImageRepByConvertingToColorSpace:NSColorSpace.sRGBColorSpace
                               renderingIntent:NSColorRenderingIntentDefault];
    NSData *png = [(tagged ?: sheet) representationUsingType:NSBitmapImageFileTypePNG
                                                  properties:@{}];
    return png != nil && [png writeToFile:path atomically:YES];
}

int NotifierDrawInvaderPNG(const char *path, unsigned int rgb, double size) {
    if (path == NULL) return 0;
    @autoreleasepool {
        return drawInvaderPNG(size, (uint32_t)rgb, @(path)) ? 1 : 0;
    }
}

int NotifierWriteIconset(const char *directory) {
    if (directory == NULL) return 0;
    @autoreleasepool {
        NSString *into = @(directory);
        // The ten an .iconset holds. iconutil refuses one that is short.
        NSArray *wanted = @[@[@"icon_16x16.png", @16], @[@"icon_16x16@2x.png", @32],
                            @[@"icon_32x32.png", @32], @[@"icon_32x32@2x.png", @64],
                            @[@"icon_128x128.png", @128], @[@"icon_128x128@2x.png", @256],
                            @[@"icon_256x256.png", @256], @[@"icon_256x256@2x.png", @512],
                            @[@"icon_512x512.png", @512], @[@"icon_512x512@2x.png", @1024]];
        for (NSArray *one in wanted) {
            // Iris: the app icon is the PROGRAM and deliberately not one of the
            // hues a state wears. An icon in Love would read as an agent
            // waiting for you every time a banner appeared.
            if (!drawInvaderPNG([one[1] doubleValue], 0xc4a7e7,
                          [into stringByAppendingPathComponent:one[0]])) {
                return 0;
            }
        }
        return 1;
    }
}



// ── the delegate ─────────────────────────────────────────────────────────────

@interface AgentNotifyBanners : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation AgentNotifyBanners

// Shown even though this app is the frontmost one — it never is, being an
// accessory, but the default without this is to suppress a notification from
// whoever is in front, and "whoever is in front" is not a thing an accessory
// app should ever be reasoning about.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))done {
    done(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList |
         UNNotificationPresentationOptionSound);
}

// Tapping a notification goes exactly where choosing the row goes.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
didReceiveNotificationResponse:(UNNotificationResponse *)response
         withCompletionHandler:(void (^)(void))done {
    NSString *key = response.notification.request.content.userInfo[@"key"];
    if (key.length > 0) goBannerWasTapped((char *)[key UTF8String]);
    done();
}

@end

// ── what Go calls ────────────────────────────────────────────────────────────

int NotifierAvailable(void) {
    CFDictionaryRef session = CGSessionCopyCurrentDictionary();
    if (session == NULL) return 0;
    CFRelease(session);
    return 1;
}

int NotifierStart(void) {
    @autoreleasepool {
        // This process has to BE an application, and that is not a formality
        // [measured 2026-09-18, macOS 26.6.2]. Without this line `NSApp` is nil,
        // `[NSApp run]` is a message to nil and returns at once, the main queue
        // is never drained — and so nothing macOS hands back is ever looked at.
        // Posting still works, because that is XPC and not a callback, which is
        // what makes the failure invisible: banners arrive, and clicking one
        // says "the application is not responding properly" while a SECOND
        // instance is launched behind it to answer.
        //
        // Accessory: no Dock tile, no menu of its own, no stealing the
        // foreground — but activatable, which it must be to receive a tap.
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        // Asked SECOND, and this order is the whole of the guard: without a
        // bundle identifier `currentNotificationCenter` does not fail, it
        // raises inside a dispatch_once and terminates the process
        // [verified 2026-09-18]. A program that killed itself trying to be
        // helpful would be the worst possible reading of R13.
        if ([[NSBundle mainBundle] bundleIdentifier] == nil) return 0;

        if (watcher == nil) watcher = [[AgentNotifyBanners alloc] init];

        UNUserNotificationCenter *centre = [UNUserNotificationCenter currentNotificationCenter];
        centre.delegate = watcher;
        // Provisional is on this list and it is what makes the difference
        // between notifications working and not [verified 2026-09-18, macOS
        // 26.6.2]. Asking for Alert alone is answered "Notifications are not
        // allowed for this application" — no prompt, no entry in System
        // Settings, nowhere to go and turn it on. Adding Provisional is granted
        // immediately and without asking anybody: the notifications arrive
        // quietly, in Notification Centre rather than as banners, and the app
        // finally APPEARS in System Settings → Notifications, where a person
        // can promote it to banners and sound if they want them.
        //
        // Quiet-by-default is also the right first behaviour for something
        // nobody asked to be interrupted by.
        [centre requestAuthorizationWithOptions:(UNAuthorizationOptionAlert |
                                                 UNAuthorizationOptionSound |
                                                 UNAuthorizationOptionProvisional)
                              completionHandler:^(BOOL granted, NSError *problem) {
            // Nothing to do either way. Being refused is a legitimate answer
            // and the person can change it in System Settings; failing to ask
            // is not this program's problem to solve at three in the morning.
            (void)granted; (void)problem;
        }];
        return 1;
    }
}

void NotifierPost(const char *key, const char *title, const char *subtitle,
                  const char *body, const char *picture, int sound,
                  const char *soundName) {
    if (key == NULL || title == NULL) return;
    @autoreleasepool {
        // The same guard NotifierStart has, and it is not belt and braces:
        // `currentNotificationCenter` terminates the process when there is no
        // bundle identifier, and this is a second way to reach it. Found by the
        // test that posts from the bare test binary — which is the only place
        // the bare case can be reached at all, and therefore the only place it
        // could be found.
        if ([[NSBundle mainBundle] bundleIdentifier] == nil) return;

        NSString *session = @(key);
        UNMutableNotificationContent *content = [[UNMutableNotificationContent alloc] init];
        content.title = @(title);
        if (subtitle != NULL && strlen(subtitle) > 0) content.subtitle = @(subtitle);
        if (body != NULL && strlen(body) > 0) content.body = @(body);
        // No sound on the content is silence, whatever the presentation
        // options below say they would allow: `UNNotificationPresentationOptionSound`
        // plays the sound a notification HAS, and this one has none.
        if (!sound) {
            content.sound = nil;
        } else if (soundName != NULL && strlen(soundName) > 0) {
            content.sound = [UNNotificationSound soundNamed:@(soundName)];
        } else {
            content.sound = [UNNotificationSound defaultSound];
        }
        content.userInfo = @{@"key": session};

        // The mark, in the colour of whatever just happened. An attachment is
        // the only per-notification picture there is — the app icon is fixed
        // for the whole app — and macOS shows it beside the text.
        //
        // It TAKES the file: the attachment is moved into the notification
        // system's own store when the request is added, so the path handed in
        // here is gone afterwards and whoever drew it has to be able to draw it
        // again (marks.go).
        if (picture != NULL && strlen(picture) > 0) {
            NSError *problem = nil;
            UNNotificationAttachment *mark =
                [UNNotificationAttachment attachmentWithIdentifier:@"mark"
                                                               URL:[NSURL fileURLWithPath:@(picture)]
                                                           options:nil
                                                             error:&problem];
            // A banner with no picture is a banner; a banner nobody posted is
            // not. Whatever went wrong with the file, the words still go out.
            if (mark != nil) content.attachments = @[mark];
        }

        // The session is the identifier, so a session that changes state twice
        // replaces its own notification instead of stacking a second one under
        // it. A semaphore with eight banners about one agent is a worse
        // semaphore than no banners at all.
        UNNotificationRequest *request =
            [UNNotificationRequest requestWithIdentifier:session content:content trigger:nil];
        [[UNUserNotificationCenter currentNotificationCenter]
            addNotificationRequest:request withCompletionHandler:nil];
    }
}

char *NotifierStatus(void) {
    @autoreleasepool {
        if ([[NSBundle mainBundle] bundleIdentifier] == nil) return NULL;
        __block NSString *answer = nil;
        dispatch_semaphore_t waited = dispatch_semaphore_create(0);
        [[UNUserNotificationCenter currentNotificationCenter]
            getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
                switch (settings.authorizationStatus) {
                    case UNAuthorizationStatusNotDetermined: answer = @"not-determined"; break;
                    case UNAuthorizationStatusDenied:        answer = @"denied"; break;
                    case UNAuthorizationStatusAuthorized:    answer = @"authorized"; break;
                    case UNAuthorizationStatusProvisional:   answer = @"provisional"; break;
                    default:                                 answer = @"unknown"; break;
                }
                dispatch_semaphore_signal(waited);
            }];
        if (dispatch_semaphore_wait(waited,
                dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC)) != 0) {
            return NULL;
        }
        return strdup([answer UTF8String]);
    }
}

char *NotifierBundleIdentifier(void) {
    @autoreleasepool {
        NSString *identifier = [[NSBundle mainBundle] bundleIdentifier];
        return identifier ? strdup([identifier UTF8String]) : NULL;
    }
}

void NotifierRun(void) {
    @autoreleasepool {
        [NSApp run];
    }
}

void NotifierStop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        // `stop:` takes effect when the loop next comes round, and a loop with
        // nothing happening in it never does. This is the event that wakes it.
        [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                            location:NSZeroPoint
                                       modifierFlags:0
                                           timestamp:0
                                        windowNumber:0
                                             context:nil
                                             subtype:0
                                               data1:0
                                               data2:0]
                 atStart:YES];
    });
}

int NotifierAnswers(double seconds) {
    @autoreleasepool {
        // Does anything this process is handed ever get looked at? A block onto
        // the main queue answers it, and nothing else does: the notification
        // centre, the tap on a banner and every other callback macOS has all
        // arrive that way, so a process whose main queue is not being drained
        // is a process that posts and then plays dead.
        //
        // Callable from any thread on purpose — it is asked BY the thread that
        // wants to know, about the one that is supposed to be running the loop.
        dispatch_semaphore_t answered = dispatch_semaphore_create(0);
        dispatch_async(dispatch_get_main_queue(), ^{
            dispatch_semaphore_signal(answered);
        });
        return dispatch_semaphore_wait(answered,
            dispatch_time(DISPATCH_TIME_NOW, (int64_t)(seconds * NSEC_PER_SEC))) == 0;
    }
}

void NotifierPump(double seconds) {
    @autoreleasepool {
        [[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:seconds]];
    }
}
