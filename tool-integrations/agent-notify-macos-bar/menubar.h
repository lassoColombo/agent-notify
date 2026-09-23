// menubar.h — the whole of what Go may ask AppKit to do.
//
// Six functions and one callback, and that is deliberate: everything that
// decides what should be on the screen is on the Go side of this file, and what
// is on this side applies a document it is given. See paint.go.

#ifndef AGENT_NOTIFY_MENUBAR_H
#define AGENT_NOTIFY_MENUBAR_H

// MenuBarBundleIdentifier is the identifier of the bundle this process is
// running inside, or NULL when it is a bare binary — which is worth knowing,
// because a bare binary has no preferences domain that survives it and
// therefore cannot remember where the person put its item. The caller owns the
// string.
char *MenuBarBundleIdentifier(void);

// MenuBarAvailable reports whether there is a window server to draw on. It is
// asked BEFORE MenuBarStart because AppKit's answer to being started without
// one is to abort the process, and a display that cannot draw should say so and
// exit rather than die.
int MenuBarAvailable(void);

// MenuBarStart puts the item on the bar. It MUST be called on the main thread,
// before MenuBarRun, and once. `font` may be NULL for the menu bar's own.
void MenuBarStart(const char *font);

// MenuBarApply draws a Paint, given as JSON. Safe from any thread: it parses
// where it is called and applies on the main queue.
void MenuBarApply(const char *json);

// MenuBarRun is the AppKit run loop. It does not return until MenuBarStop.
void MenuBarRun(void);

// MenuBarStop ends the run loop and takes the item off the bar.
void MenuBarStop(void);

// MenuBarPump runs the loop for a while and returns. It exists for tests, which
// need the bar to have serviced what they asked for without giving up their
// thread for ever.
void MenuBarPump(double seconds);

// MenuBarOnTheBar is what the accessibility API says this process is showing in
// the menu bar, one title per line. NULL when it cannot be asked, which on a
// fresh machine means nobody has granted accessibility to whatever is
// responsible for this process. The caller owns the string.
//
// It is the only way to check a status item from a program: NSStatusItem
// windows are hosted out of process and never appear in
// CGWindowListCopyWindowInfo [verified 2026-09-18, macOS 26.6.2].
char *MenuBarOnTheBar(void);

// MenuBarColourKnown reports whether AppKit has a colour of that name, so that
// a colour nobody can draw is refused when the config is read rather than
// silently ignored at the first paint.
int MenuBarColourKnown(const char *name);

// MenuBarWriteIconset draws the app's icon into `directory` as the ten PNGs an
// .iconset is made of. Returns 0 if anything could not be written.
//
// Drawn rather than shipped: an icon checked into a repository is a binary
// nobody can review in a diff, and this one is a rounded rectangle and a symbol
// the system already has.
int MenuBarWriteIconset(const char *directory);

// MenuBarSymbolKnown reports whether this macOS has an SF Symbol of that name.
// The symbol set grows every release and a name from a newer one draws nothing
// at all, so a display built elsewhere must be able to find out rather than
// leave a hole where a state's icon should be.
int MenuBarSymbolKnown(const char *name);

// MenuBarFontKnown reports whether a font of that name can be had, so that a
// name nobody can draw with is refused when the config is read rather than
// quietly replaced by the menu bar's own font.
int MenuBarFontKnown(const char *name);

#endif
