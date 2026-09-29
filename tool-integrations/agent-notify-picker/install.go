package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// The table is a heading: no `binary`, because core must never run a picker,
// and the path lives in the keybinding, which is the one thing that runs it.
func table() string {
	return `# Run when you press a key, and never by core: it has no binary here, which is
# what says so. The path lives in the keybinding, which is the thing that runs it.
[integration.picker]
`
}

// keybinding is how zellij is asked to open it: a floating pane that closes
// when the picker exits. Printed, never written: a zellij config is KDL, full
// of somebody's own comments and key choices.
func keybinding(program string) string {
	return fmt.Sprintf(`bind "Alt a" {
    Run %q {
        name "agents"
        floating true
        close_on_exit true
        width "90%%"
        height "90%%"
        x "5%%"
        y 1
    }
}`, program)
}

// install files the table and prints the keybinding, which is KDL in a file
// of yours (D-66, D-85).
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

func printTable(out, problems io.Writer, arguments []string) int {
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", Name, err)
		return 1
	}
	return me.Install(subscribe.Install{
		Table: func(string, string) string { return table() },
		Advice: "\nAnd this in zellij's config.kdl to open it with a key:\n\n" + indent(keybinding(program)) +
			"\nThe path is in the keybinding and not in the table, because a table with a\n" +
			"binary in it is one core may run, and nothing core runs has a terminal.\n",
	}, out, problems, arguments)
}

func uninstall(arguments []string) int {
	return me.Uninstall(os.Stdout, os.Stderr, arguments)
}

func indent(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		out.WriteString("    " + line + "\n")
	}
	return out.String()
}
