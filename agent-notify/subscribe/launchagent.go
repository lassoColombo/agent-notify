package subscribe

import (
	"fmt"
	"html"
)

// LaunchAgentPlist is the launchd job that keeps a display running, printed for
// somebody to put in ~/Library/LaunchAgents and never written by us (D-66).
//
// Something has to start a display that owns its own process, and on macOS that
// is launchd: it starts the job when you log in, restarts it if it dies, and
// runs it inside your logged-in session — which a menu bar item and a
// notification both require and a system daemon cannot give them.
//
// `KeepAlive` is the whole reason not to hand-roll this with a login item. A
// display that crashes comes back; one whose core was reinstalled underneath it
// comes back; and neither costs the person anything to notice.
//
// It is printed rather than installed for the reason D-66 gives about the
// config table, and the reason is stronger here: loading a launch agent is
// putting a program in your login session for ever, which is not a thing a
// program should arrange for itself while you are reading its output.
func LaunchAgentPlist(label, binary string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, html.EscapeString(label), html.EscapeString(binary))
}

// HowToLoadTheLaunchAgent is what to do with what [LaunchAgentPlist] printed.
//
// Said out loud because a plist in the right directory does nothing until it is
// loaded, and the failure — everything installed, nothing running, no error
// anywhere — is the one this project keeps having to write sentences about.
func HowToLoadTheLaunchAgent(label string) string {
	return fmt.Sprintf(`Put it in ~/Library/LaunchAgents/%s.plist and load it:

  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/%s.plist

It starts now and at every login. To stop it:

  launchctl bootout gui/$(id -u)/%s
`, label, label, label)
}
