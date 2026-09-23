// notifier.h — the whole of what Go may ask macOS to do.
//
// Everything that decides WHETHER to say something is on the Go side of this
// file (notify.go); what is on this side posts what it is handed and answers
// questions about what macOS has decided.

#ifndef AGENT_NOTIFY_NOTIFIER_H
#define AGENT_NOTIFY_NOTIFIER_H

// NotifierBundleIdentifier is the identifier of the bundle this process is
// running inside, or NULL when it is a bare binary — which is the difference
// between a program that can notify and one that cannot. The caller owns the
// string.
char *NotifierBundleIdentifier(void);

// NotifierAvailable reports whether there is a logged-in window session. It is
// asked BEFORE NotifierStart because AppKit's answer to being started without
// one is to abort the process, and a program that cannot notify should say so
// and exit rather than die.
int NotifierAvailable(void);

// NotifierStart asks macOS for permission and returns whether there is any
// point: 0 when this is a bare binary, because UNUserNotificationCenter does
// not return an error without a bundle identifier — it aborts the process. It
// MUST be called on the main thread, before NotifierRun, and once.
//
// The answer to the permission itself arrives later and asynchronously, and
// nothing waits for it: a notification posted before the person has answered is
// queued or dropped by macOS, which is macOS's business and not this
// integration's (R13).
int NotifierStart(void);

// NotifierPost posts one. `key` is the session, and it is also the
// notification's identifier, so a second notification about the same session
// REPLACES the first rather than stacking under it. Tapping one focuses that
// session. Safe from any thread.
//
// `picture` is a PNG to show beside the text, or NULL. macOS MOVES the file
// into its own store, so it is gone afterwards and the caller must be able to
// produce it again.
//
// `sound` is whether to attach one at all, and `soundName` which — the filename
// of a sound macOS can find, or NULL for the system default. They are
// parameters rather than something this side remembers, because nothing on this
// side of the boundary decides anything: the config is read in Go and what
// arrives here is a decision already taken.
//
// A name is NOT checked here and cannot be: UNNotificationSound accepts any
// string and reports nothing, and one it cannot resolve becomes the default
// chime. Whatever arrives has already been found on disk (sounds.go).
void NotifierPost(const char *key, const char *title, const char *subtitle,
                  const char *body, const char *picture, int sound,
                  const char *soundName);

// NotifierStatus is what macOS has decided about notifications from this
// bundle: "not-determined" (nobody has been asked yet), "denied", "authorized",
// "provisional", or "" when it could not be discovered. The caller owns the
// string.
//
// It is worth asking, because a refusal is completely silent: a program that
// posts notifications nobody ever sees looks exactly like one that is broken.
char *NotifierStatus(void);

// NotifierRun is the AppKit run loop. It does not return until NotifierStop.
void NotifierRun(void);

// NotifierStop ends the run loop.
void NotifierStop(void);

// NotifierAnswers reports whether this process's main queue is being drained,
// which is the difference between a program that can be talked to and one that
// only talks. Every callback macOS has — a tap on a banner most of all — arrives
// on that queue, and a program that is not draining it posts notifications and
// then plays dead. Safe from any thread.
int NotifierAnswers(double seconds);

// NotifierPump runs the loop for a while and returns. It exists for tests, and
// for `check`, which has to give macOS a moment to answer a question it answers
// on another queue.
void NotifierPump(double seconds);

// NotifierDrawInvaderPNG draws the mark at one size, in one colour (0xRRGGBB), to a
// PNG. It is what puts an alien the colour of the state on a banner.
int NotifierDrawInvaderPNG(const char *path, unsigned int rgb, double size);

// NotifierWriteIconset draws the app's icon into `directory` as the ten PNGs an
// .iconset is made of. Returns 0 if anything could not be written.
//
// Drawn rather than shipped: an icon checked into a repository is a binary
// nobody can review in a diff, and this one is a rounded rectangle and a sprite
// eleven cells across. It matters more here than anywhere else in this system —
// it is the picture on every banner.
int NotifierWriteIconset(const char *directory);

#endif
