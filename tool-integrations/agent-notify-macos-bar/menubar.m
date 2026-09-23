// menubar.m — the AppKit half, which makes no decisions.
//
// It takes a Paint (paint.go) as JSON and draws it. Every question about what
// should be on the screen — which sessions, in what order, what the item says
// while something is being announced — was answered in Go before this file was
// handed anything. What is here is the part that cannot be: an NSStatusItem, a
// menu built when somebody opens it, and the accessibility query that is the
// only way to find out whether any of it worked.

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>

#include "menubar.h"
#include "_cgo_export.h"

static NSStatusItem *bar = nil;
static NSMenu *dropdown = nil;
static NSArray *document = nil;
static NSFont *drawnIn = nil;
static id watcher = nil;

// gap is the space between the longest session name and the right edge the ages
// are aligned against. It is the one number in this file that is a judgement
// rather than a fact from the document, and it is here rather than in the Paint
// because it is measured in points, which Go has no business knowing about.
static const CGFloat gap = 28.0;

// ── colours ──────────────────────────────────────────────────────────────────
//
// Two spellings. `0xAARRGGBB` is what every other display in this system takes,
// and a name is one of AppKit's own — which is the spelling that survives
// somebody switching to light mode, and therefore the one the defaults use.
//
// The names are a table rather than a `performSelector:`, so that an unknown
// one is a thing that can be reported when the config is read instead of a
// crash at the first paint.
static NSColor *named(NSString *name) {
    static NSDictionary *table = nil;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        table = @{
            @"systemRed": NSColor.systemRedColor,
            @"systemOrange": NSColor.systemOrangeColor,
            @"systemYellow": NSColor.systemYellowColor,
            @"systemGreen": NSColor.systemGreenColor,
            @"systemMint": NSColor.systemMintColor,
            @"systemTeal": NSColor.systemTealColor,
            @"systemCyan": NSColor.systemCyanColor,
            @"systemBlue": NSColor.systemBlueColor,
            @"systemIndigo": NSColor.systemIndigoColor,
            @"systemPurple": NSColor.systemPurpleColor,
            @"systemPink": NSColor.systemPinkColor,
            @"systemBrown": NSColor.systemBrownColor,
            @"systemGray": NSColor.systemGrayColor,
            @"labelColor": NSColor.labelColor,
            @"secondaryLabelColor": NSColor.secondaryLabelColor,
            @"tertiaryLabelColor": NSColor.tertiaryLabelColor,
            @"quaternaryLabelColor": NSColor.quaternaryLabelColor,
            @"controlAccentColor": NSColor.controlAccentColor,
        };
    });
    return table[name];
}

static NSColor *colour(id spec, NSColor *fallback) {
    if (![spec isKindOfClass:NSString.class] || [spec length] == 0) return fallback;
    NSString *text = (NSString *)spec;
    if ([text hasPrefix:@"0x"] || [text hasPrefix:@"0X"]) {
        unsigned long long packed = strtoull([text UTF8String] + 2, NULL, 16);
        return [NSColor colorWithSRGBRed:((packed >> 16) & 0xff) / 255.0
                                   green:((packed >> 8) & 0xff) / 255.0
                                    blue:(packed & 0xff) / 255.0
                                   alpha:((packed >> 24) & 0xff) / 255.0];
    }
    NSColor *known = named(text);
    return known ? known : fallback;
}

int MenuBarColourKnown(const char *name) {
    if (name == NULL) return 0;
    NSString *text = @(name);
    if ([text hasPrefix:@"0x"] || [text hasPrefix:@"0X"]) {
        return text.length == 10 ? 1 : 0;
    }
    return named(text) != nil ? 1 : 0;
}

// ── symbols ──────────────────────────────────────────────────────────────────

// symbol is an SF Symbol, tinted, at the size the surrounding text is drawn at.
//
// nil when this version of macOS has never heard of the name, which is a real
// case worth handling rather than asserting away: the symbol set grows every
// release, and a display built against a newer one must degrade to its text
// glyph rather than draw a hole.
// The mark this program wears, on the icon and on the bar: the invader, 11
// across and 9 down.
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

// invaderName is the name a paint asks for the sprite by. It is not an SF
// Symbol and never will be, so it is spelled like one and intercepted.
static NSString *const invaderName = @"invader";

// snapped puts a rectangle on whole DEVICE pixels, whatever the scale of the
// screen it is about to be drawn on.
//
// It rounds the edges rather than the origin and the size, which is what keeps
// cells that share an edge sharing it afterwards — round the width of each and
// they drift apart into a grid with hairlines through it. The whole difference
// between pixel art and a blurry drawing of pixel art is here.
static NSRect snapped(CGContextRef context, NSRect rect) {
    if (context == NULL) return rect;
    CGRect device = CGContextConvertRectToDeviceSpace(context, rect);
    CGFloat left = roundf(CGRectGetMinX(device)), right = roundf(CGRectGetMaxX(device));
    CGFloat low = roundf(CGRectGetMinY(device)), high = roundf(CGRectGetMaxY(device));
    if (right <= left) right = left + 1;
    if (high <= low) high = low + 1;
    return CGContextConvertRectToUserSpace(context,
        CGRectMake(left, low, right - left, high - low));
}

// invaderImage is the sprite as a picture, sized to sit in text of `points`.
//
// Drawn through a handler rather than into a bitmap so that it is redrawn — and
// re-snapped — on whatever screen the menu bar has moved to, which is the only
// way a sprite survives a laptop being plugged into a 1x monitor.
static NSImage *invaderImage(NSColor *tint, CGFloat points) {
    CGFloat cell = points * 0.117;
    NSSize box = NSMakeSize(cell * invaderColumns, cell * invaderRows);
    NSImage *drawn = [NSImage imageWithSize:box flipped:NO drawingHandler:^BOOL(NSRect rect) {
        CGContextRef context = NSGraphicsContext.currentContext.CGContext;
        [tint set];
        for (int row = 0; row < invaderRows; row++) {
            for (int column = 0; column < invaderColumns; column++) {
                if (invader[row][column] != 'X') continue;
                NSRectFill(snapped(context, NSMakeRect(
                    rect.origin.x + column * cell,
                    rect.origin.y + (invaderRows - 1 - row) * cell, cell, cell)));
            }
        }
        return YES;
    }];
    drawn.template = NO;
    return drawn;
}

static NSImage *symbol(id name, NSColor *tint, CGFloat size) {
    if (![name isKindOfClass:NSString.class] || [name length] == 0) return nil;
    if ([(NSString *)name isEqualToString:invaderName]) {
        return invaderImage(tint ?: NSColor.labelColor, size);
    }
    NSImage *image = [NSImage imageWithSystemSymbolName:(NSString *)name
                               accessibilityDescription:nil];
    if (image == nil) return nil;

    image = [image imageWithSymbolConfiguration:
        [NSImageSymbolConfiguration configurationWithPointSize:size
                                                        weight:NSFontWeightRegular
                                                         scale:NSImageSymbolScaleMedium]];
    if (tint == nil) return image;

    // The colour is painted ON TOP of the drawn symbol, through source-atop,
    // rather than asked for with `configurationWithPaletteColors:`. They are not
    // the same picture, and the difference is the whole legibility of this
    // display [measured 2026-09-18, macOS 26.6.2]: a palette colour FILLS THE
    // KNOCKOUT. `questionmark.circle.fill` is a disc with a question mark cut
    // out of it, and a middle scanline of the rasterised image goes
    //
    //     palette:     ..############################..
    //     source-atop: ..#############..#############..
    //
    // — so every `.fill` symbol arrived on the menu bar as a coloured blob, a
    // red circle and an orange triangle that said nothing about which state they
    // were. Source-atop leaves the destination alpha alone, which is what keeps
    // the hole a hole.
    //
    // Drawn through a handler rather than into a locked-focus bitmap so the
    // result stays resolution-independent: this is redrawn at whatever scale the
    // display it lands on asks for, and a menu bar is a place where a 1x
    // rasterisation is immediately obvious.
    NSSize box = image.size;
    NSImage *tinted = [NSImage imageWithSize:box flipped:NO drawingHandler:^BOOL(NSRect rect) {
        [image drawInRect:rect];
        [tint set];
        NSRectFillUsingOperation(rect, NSCompositingOperationSourceAtop);
        return YES;
    }];
    // Not a template any more: it carries its own colour, and a template would
    // be repainted by whatever draws it.
    tinted.template = NO;
    return tinted;
}

// inline puts a symbol into a run of text, sitting on the text's baseline.
//
// The bounds are set by hand because an attachment is laid out from the bottom
// of the line by default, which puts an icon a couple of points too high next
// to digits.
static NSAttributedString *inlineSymbol(NSImage *image, NSFont *font) {
    NSTextAttachment *attachment = [[NSTextAttachment alloc] init];
    attachment.image = image;
    CGFloat height = image.size.height;
    attachment.bounds = NSMakeRect(0, roundf(font.descender / 2.0), image.size.width, height);
    return [NSAttributedString attributedStringWithAttachment:attachment];
}

// ── the menu ─────────────────────────────────────────────────────────────────

@interface AgentNotifyBar : NSObject <NSMenuDelegate>
@end

@implementation AgentNotifyBar

// The menu is built HERE, when somebody opens it, and not when a paint arrives.
// A paint happens whenever any agent anywhere does anything; a menu is open for
// a second or two a day. Building it on open is less work, and it is also the
// only way not to rebuild a menu underneath somebody who is reading it.
- (void)menuNeedsUpdate:(NSMenu *)menu {
    [menu removeAllItems];
    NSArray *rows = document;
    if (rows.count == 0) return;

    NSDictionary *plain = @{NSFontAttributeName: drawnIn};
    CGFloat widest = 0, ages = 0;
    for (NSDictionary *row in rows) {
        if (![row[@"kind"] isEqualToString:@"row"]) continue;
        NSString *left = [NSString stringWithFormat:@"%@ %@", row[@"glyph"] ?: @"", row[@"text"] ?: @""];
        widest = MAX(widest, [left sizeWithAttributes:plain].width);
        NSString *age = row[@"age"] ?: @"";
        ages = MAX(ages, [age sizeWithAttributes:plain].width);
    }

    NSMutableParagraphStyle *columns = [[NSMutableParagraphStyle alloc] init];
    columns.tabStops = @[[[NSTextTab alloc] initWithTextAlignment:NSTextAlignmentRight
                                                         location:widest + gap + ages
                                                          options:@{}]];

    for (NSDictionary *row in rows) {
        NSString *kind = row[@"kind"];
        NSString *text = row[@"text"] ?: @"";
        NSMenuItem *entry;
        NSImage *rowImage = nil;

        if ([kind isEqualToString:@"separator"]) {
            [menu addItem:[NSMenuItem separatorItem]];
            continue;
        }

        if ([kind isEqualToString:@"section"]) {
            if ([NSMenuItem respondsToSelector:@selector(sectionHeaderWithTitle:)]) {
                entry = [NSMenuItem sectionHeaderWithTitle:text];
            } else {
                entry = [[NSMenuItem alloc] initWithTitle:text action:NULL keyEquivalent:@""];
                entry.enabled = NO;
            }
            [menu addItem:entry];
            continue;
        }

        if ([kind isEqualToString:@"note"]) {
            entry = [[NSMenuItem alloc] initWithTitle:text action:NULL keyEquivalent:@""];
            entry.attributedTitle = [[NSAttributedString alloc]
                initWithString:text
                    attributes:@{NSFontAttributeName: drawnIn,
                                 NSForegroundColorAttributeName: NSColor.secondaryLabelColor}];
            entry.enabled = NO;
            [menu addItem:entry];
            continue;
        }

        NSColor *hue = colour(row[@"colour"], NSColor.labelColor);
        BOOL lit = [row[@"lit"] boolValue];
        NSMutableAttributedString *title = [[NSMutableAttributedString alloc] init];

        // The state's icon goes in the item's IMAGE, not into its text. That is
        // where AppKit draws an icon in a menu and where every other menu on
        // the system puts one, so the rows line up with the rest of macOS
        // instead of with themselves.
        NSImage *mark = symbol(row[@"symbol"], hue, drawnIn.pointSize);
        if (mark != nil) {
            rowImage = mark;
        } else {
            rowImage = nil;
            NSString *glyph = row[@"glyph"] ?: @"";
            if (glyph.length > 0) {
                [title appendAttributedString:[[NSAttributedString alloc]
                    initWithString:[glyph stringByAppendingString:@" "]
                        attributes:@{NSFontAttributeName: drawnIn,
                                     NSForegroundColorAttributeName: hue}]];
            }
        }
        [title appendAttributedString:[[NSAttributedString alloc]
            initWithString:text
                attributes:@{NSFontAttributeName: lit ? [NSFont boldSystemFontOfSize:drawnIn.pointSize] : drawnIn,
                             NSForegroundColorAttributeName: lit ? hue : NSColor.labelColor}]];
        NSString *age = row[@"age"] ?: @"";
        if (age.length > 0) {
            [title appendAttributedString:[[NSAttributedString alloc]
                initWithString:[@"\t" stringByAppendingString:age]
                    attributes:@{NSFontAttributeName: drawnIn,
                                 NSForegroundColorAttributeName: NSColor.tertiaryLabelColor}]];
        }
        [title addAttribute:NSParagraphStyleAttributeName value:columns
                      range:NSMakeRange(0, title.length)];


        entry = [[NSMenuItem alloc] initWithTitle:text action:@selector(chose:) keyEquivalent:@""];
        entry.attributedTitle = title;
        entry.image = rowImage;
        entry.target = self;
        entry.representedObject = row[@"key"];
        entry.enabled = row[@"key"] != nil;
        [menu addItem:entry];
    }
}

// chose hands the session straight back to Go, which is where every decision
// about what choosing one means already lives — including that it must not
// happen on this thread, because focusing a session runs a program and the menu
// is still tracking.
- (void)chose:(NSMenuItem *)sender {
    NSString *key = sender.representedObject;
    if (key.length == 0) return;
    goMenuRowWasChosen((char *)[key UTF8String]);
}

@end

// ── what Go calls ────────────────────────────────────────────────────────────

char *MenuBarBundleIdentifier(void) {
    @autoreleasepool {
        NSString *identifier = [[NSBundle mainBundle] bundleIdentifier];
        return identifier ? strdup([identifier UTF8String]) : NULL;
    }
}

// ── the app icon ─────────────────────────────────────────────────────────────

// drawIcon renders one size of it: a rounded plum tile with the invader on it,
// which is the same mark this program puts everywhere else it appears.
//
// The corner radius is 0.2237 of the width, which is the ratio macOS uses, and
// the 5.5% inset is the margin every app icon leaves so that its corners are
// not flush with the next icon's in the Dock.
static BOOL drawIcon(CGFloat size, NSString *path) {
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
        initWithStartingColor:colour(@"0xff26233a", NSColor.blackColor)   // Overlay
                  endingColor:colour(@"0xff191724", NSColor.blackColor)]; // Base
    [night drawInBezierPath:rounded angle:-90.0];

    // Iris, and deliberately neither of the two hues the states use: an icon
    // that borrowed Love or Gold would read as a state rather than as the
    // program. Iris is the palette's structural colour and belongs to nothing
    // that happens.
    [colour(@"0xffc4a7e7", NSColor.whiteColor) set];

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

int MenuBarWriteIconset(const char *directory) {
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
            if (!drawIcon([one[1] doubleValue],
                          [into stringByAppendingPathComponent:one[0]])) {
                return 0;
            }
        }
        return 1;
    }
}

int MenuBarSymbolKnown(const char *name) {
    if (name == NULL || strlen(name) == 0) return 1;
    @autoreleasepool {
        if ([@(name) isEqualToString:invaderName]) return 1;
        return [NSImage imageWithSystemSymbolName:@(name) accessibilityDescription:nil] != nil;
    }
}

int MenuBarFontKnown(const char *name) {
    if (name == NULL || strlen(name) == 0) return 1;
    @autoreleasepool {
        return [NSFont fontWithName:@(name) size:13.0] != nil ? 1 : 0;
    }
}

int MenuBarAvailable(void) {
    CFDictionaryRef session = CGSessionCopyCurrentDictionary();
    if (session == NULL) return 0;
    CFRelease(session);
    return 1;
}

void MenuBarStart(const char *font) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Accessory: no Dock tile, no menu of our own, no stealing the
        // foreground. LSUIElement in a bundle's Info.plist says the same thing,
        // and this display has no bundle to put it in.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        drawnIn = [NSFont menuFontOfSize:0];
        if (font != NULL && strlen(font) > 0) {
            NSFont *asked = [NSFont fontWithName:@(font) size:drawnIn.pointSize];
            if (asked != nil) drawnIn = asked;
        }

        watcher = [[AgentNotifyBar alloc] init];
        bar = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        // Where the item sits is the person's to decide, by cmd-dragging it,
        // and this is what makes that decision survive. Without it AppKit has
        // nothing to file the position under, so every restart of this display
        // re-adds the item at the FAR LEFT of the menu bar — which on a machine
        // running Hidden Bar, Bartender or Ice means straight back into the
        // collapsed section, every time, no matter where it was put.
        bar.autosaveName = @"agent-notify";
        dropdown = [[NSMenu alloc] init];
        dropdown.delegate = watcher;
        // Off, because this menu says what is enabled itself: a section header
        // and a note are deliberately dead, and AppKit's own rule — enabled if
        // something responds to the action — would light them up.
        dropdown.autoenablesItems = NO;
        bar.menu = dropdown;
        bar.button.title = @"…";
    }
}

// spans is the title the last paint asked for, kept because the flicker redraws
// it. It is the only thing this file remembers about a paint, and it remembers
// it for the same reason a screen saver remembers the desktop.
static NSArray *spans = nil;

// faded is a colour at a fraction of the opacity it came with.
static NSColor *faded(NSColor *hue, CGFloat opacity) {
    if (opacity >= 1.0 || hue == nil) return hue;
    NSColor *resolved = [hue colorUsingColorSpace:NSColorSpace.sRGBColorSpace];
    if (resolved == nil) return [hue colorWithAlphaComponent:opacity];
    return [resolved colorWithAlphaComponent:resolved.alphaComponent * opacity];
}

// composed turns the title of a paint into what the button wears.
static NSAttributedString *composed(NSArray *pieces, CGFloat opacity) {
    NSMutableAttributedString *title = [[NSMutableAttributedString alloc] init];
    for (NSDictionary *span in pieces) {
        if (![span isKindOfClass:NSDictionary.class]) continue;
        NSString *text = span[@"text"] ?: @"";
        // Only the pieces that asked to flicker are faded. The rest sit still
        // at full strength through the whole cycle, which is the difference
        // between one agent asking for you and the item asking on behalf of
        // everything on it.
        CGFloat own = [span[@"flicker"] boolValue] ? opacity : 1.0;
        NSColor *hue = faded(colour(span[@"colour"], NSColor.labelColor), own);
        NSImage *mark = symbol(span[@"symbol"], hue, drawnIn.pointSize);

        if (mark != nil) {
            // The separator the render wrote lives on the front of the text,
            // and it belongs in front of the MARK when there is one — otherwise
            // what follows runs into it.
            NSUInteger space = 0;
            while (space < text.length && [text characterAtIndex:space] == ' ') space++;
            if (space > 0) {
                [title appendAttributedString:[[NSAttributedString alloc]
                    initWithString:[text substringToIndex:space]
                        attributes:@{NSFontAttributeName: drawnIn}]];
                text = [text substringFromIndex:space];
            }
            [title appendAttributedString:inlineSymbol(mark, drawnIn)];
            // The glyph the render also wrote is the fallback, and the symbol
            // has replaced it. Everything after it is what the span says.
            if (text.length > 0 && ![[NSCharacterSet decimalDigitCharacterSet]
                    characterIsMember:[text characterAtIndex:0]]) {
                text = [text substringFromIndex:1];
            }
            if (text.length > 0) {
                // Hair space: a mark needs less room before what follows it
                // than two letters do. Not written when nothing follows,
                // because a trailing space is width on a bar where width is the
                // scarcest thing there is — and it is also a title, to the
                // accessibility API, which would then hide the one this item
                // does have.
                [title appendAttributedString:[[NSAttributedString alloc]
                    initWithString:@"\u2009" attributes:@{NSFontAttributeName: drawnIn}]];
            }
        }
        if (text.length == 0) continue;
        [title appendAttributedString:[[NSAttributedString alloc]
            initWithString:text
                attributes:@{NSFontAttributeName: drawnIn,
                             NSForegroundColorAttributeName: hue}]];
    }
    return title;
}

// wear puts the current title on the button at a given opacity.
static void wear(CGFloat opacity) {
    if (bar == nil || spans == nil) return;
    NSAttributedString *title = composed(spans, opacity);
    if (title.length > 0) bar.button.attributedTitle = title;
}

// pulse is the flicker, and it is the one thing on the bar that moves.
//
// A timer rather than a repaint: the paint says WHETHER the item should be
// asking for you and this decides what that looks like, which keeps the
// decision on the Go side of the boundary and the animation off the wire. A
// display that flickered by repainting would send two documents a second, for
// as long as somebody left an agent waiting.
static NSTimer *pulse = nil;
static BOOL dimmed = NO;

// asking reports whether any piece of this title wants somebody. Reading a
// document is not deciding anything: WHICH pieces flicker was settled on the
// other side of the boundary (D-49).
static BOOL asking(NSArray *pieces) {
    for (NSDictionary *span in pieces) {
        if ([span isKindOfClass:NSDictionary.class] && [span[@"flicker"] boolValue]) return YES;
    }
    return NO;
}

// The title is REDRAWN at a lower opacity rather than the button being faded.
// `bar.button.alphaValue` is the obvious way and it does nothing at all
// [measured 2026-09-18, macOS 26.6.2]: the property takes the value and reads
// it back, and the item on the screen does not change, because a status item's
// window is hosted by another process and what crosses is the drawn content. A
// colour is content. An alpha on a view is not.
static void flickering(BOOL wanted) {
    if (!wanted) {
        if (pulse != nil) {
            [pulse invalidate];
            pulse = nil;
        }
        if (dimmed) {
            dimmed = NO;
            wear(1.0);
        }
        return;
    }
    if (pulse != nil) return;
    pulse = [NSTimer scheduledTimerWithTimeInterval:0.55 repeats:YES block:^(NSTimer *timer) {
        dimmed = !dimmed;
        wear(dimmed ? 0.3 : 1.0);
    }];
}

void MenuBarApply(const char *json) {
    if (json == NULL) return;
    @autoreleasepool {
        NSData *raw = [@(json) dataUsingEncoding:NSUTF8StringEncoding];
        NSDictionary *paint = [NSJSONSerialization JSONObjectWithData:raw options:0 error:NULL];
        if (![paint isKindOfClass:NSDictionary.class]) return;

        // Everything below touches AppKit and therefore belongs on the main
        // thread; the parse above does not and is done here, on whichever
        // goroutine's thread called, so the main queue gets a ready document.
        dispatch_async(dispatch_get_main_queue(), ^{
            if (bar == nil) return;
            spans = paint[@"title"];
            wear(1.0);
            bar.button.toolTip = paint[@"tooltip"];
            // The item is a picture now, and a picture has no title: without
            // this it is an unlabelled button to VoiceOver, and to the
            // accessibility API — which is the only thing that can see a status
            // item at all, and therefore the only test oracle there is.
            bar.button.accessibilityLabel = paint[@"tooltip"];
            flickering(asking(spans));
            document = paint[@"menu"];
        });
    }
}

void MenuBarRun(void) {
    @autoreleasepool {
        [NSApp run];
    }
}

void MenuBarStop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        flickering(NO);
        // The item is NOT removed here, and that is deliberate.
        //
        // `removeStatusItem:` erases the item's saved position — measured: the
        // whole preferences domain went back to not existing after one
        // `watcher reload`, because the autosave entry was the only thing in
        // it. So a display that tidied up after itself could never remember
        // where the person had put it, which is the entire reason it lives in
        // a bundle (bundle.go).
        //
        // Nothing is left behind by not calling it. A status item belongs to
        // the process that made it and goes when that process goes; there is no
        // stale-number-on-the-bar failure here of the kind the sketchybar
        // display has to clean up after, because that display draws into
        // somebody else's bar and this one does not.
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

void MenuBarPump(double seconds) {
    @autoreleasepool {
        [[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:seconds]];
    }
}

char *MenuBarOnTheBar(void) {
    @autoreleasepool {
        AXUIElementRef me = AXUIElementCreateApplication(getpid());
        CFTypeRef extras = NULL;
        AXError problem = AXUIElementCopyAttributeValue(me, CFSTR("AXExtrasMenuBar"), &extras);
        if (problem != kAXErrorSuccess || extras == NULL) {
            CFRelease(me);
            return NULL;
        }
        CFTypeRef children = NULL;
        NSMutableArray *titles = [NSMutableArray array];
        if (AXUIElementCopyAttributeValue((AXUIElementRef)extras, kAXChildrenAttribute, &children)
                == kAXErrorSuccess && children != NULL) {
            for (CFIndex i = 0; i < CFArrayGetCount((CFArrayRef)children); i++) {
                AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex((CFArrayRef)children, i);
                // Title first, description second: an item whose title is a
                // picture has no title at all, and what it has instead is the
                // accessibility label the paint gave it.
                NSString *said = nil;
                CFTypeRef value = NULL;
                if (AXUIElementCopyAttributeValue(child, kAXTitleAttribute, &value)
                        == kAXErrorSuccess && value != NULL) {
                    NSString *trimmed = [(__bridge NSString *)value
                        stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceCharacterSet];
                    if (trimmed.length > 0) said = [(__bridge NSString *)value copy];
                    CFRelease(value);
                    value = NULL;
                }
                if (said == nil &&
                    AXUIElementCopyAttributeValue(child, kAXDescriptionAttribute, &value)
                        == kAXErrorSuccess && value != NULL) {
                    said = [(__bridge NSString *)value copy];
                    CFRelease(value);
                }
                if (said != nil) [titles addObject:said];
            }
            CFRelease(children);
        }
        CFRelease(extras);
        CFRelease(me);
        return strdup([[titles componentsJoinedByString:@"\n"] UTF8String]);
    }
}
