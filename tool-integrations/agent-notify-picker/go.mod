module github.com/lassoColombo/agent-notify-picker

go 1.26.0

require (
	charm.land/glamour/v2 v2.0.1
	charm.land/lipgloss/v2 v2.0.6
	github.com/junegunn/fzf v0.74.4
	github.com/lassoColombo/agent-notify v0.0.0
	golang.org/x/term v0.34.0
)

require (
	github.com/alecthomas/chroma/v2 v2.14.0 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/charlievieth/fastwalk v1.0.14 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886 // indirect
	github.com/charmbracelet/x/ansi v0.11.8 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/gdamore/tcell/v2 v2.9.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/junegunn/go-shellwords v0.0.0-20250127100254-2aa3b3277741 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	github.com/yuin/goldmark-emoji v1.0.5 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.28.0 // indirect
)

// Core is not published yet. This line comes out in M18, when it is; until
// then an integration is built beside it and a clone of this repo alone does
// not compile, which is a thing M18 exists to fix.
replace github.com/lassoColombo/agent-notify => ../../agent-notify
