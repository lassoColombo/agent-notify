package subscribe

import (
	"fmt"
	"html"
)

// LaunchAgentPlist is the launchd job that keeps a display running: started at
// login, restarted if it dies, inside the logged-in session a menu bar or a
// notification requires. Printed, never installed: loading one puts a program
// in your login session for ever (D-66).
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
func HowToLoadTheLaunchAgent(label string) string {
	return fmt.Sprintf(`Put it in ~/Library/LaunchAgents/%s.plist and load it:

  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/%s.plist

It starts now and at every login. To stop it:

  launchctl bootout gui/$(id -u)/%s
`, label, label, label)
}
