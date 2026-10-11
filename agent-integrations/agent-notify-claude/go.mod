module github.com/lassoColombo/agent-notify-claude

go 1.26.0

require github.com/lassoColombo/agent-notify v0.0.0

require (
	golang.org/x/sys v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Core is not published yet. This line comes out in M18, when it is; until
// then an integration is built beside it and a clone of this repo alone does
// not compile, which is a thing M18 exists to fix.
replace github.com/lassoColombo/agent-notify => ../../agent-notify
