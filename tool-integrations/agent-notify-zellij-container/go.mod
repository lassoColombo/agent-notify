module github.com/lassoColombo/agent-notify-zellij-container

go 1.26.0

require (
	github.com/lassoColombo/agent-notify v0.0.0
	github.com/pelletier/go-toml/v2 v2.4.3
)

require golang.org/x/sys v0.48.0 // indirect

// Core is not published yet. This line comes out in M18, when it is; until
// then an integration is built beside it and a clone of this repo alone does
// not compile, which is a thing M18 exists to fix.
replace github.com/lassoColombo/agent-notify => ../../agent-notify
