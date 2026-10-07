package subscribe

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/lassoColombo/agent-notify/internal/config"
)

// WriteDropIn files this integration's table in `conf.d`, replacing what it
// wrote before, and says where (D-85). The user's own file is never touched.
func (i Integration) WriteDropIn(table string) (string, error) {
	layout, err := i.layout()
	if err != nil {
		return "", err
	}
	return config.WriteDropIn(layout, i.Name, table)
}

// RemoveDropIn takes the table away again.
func (i Integration) RemoveDropIn() error {
	layout, err := i.layout()
	if err != nil {
		return err
	}
	return config.RemoveDropIn(layout, i.Name)
}

// Install is what a tool-integration's `install` files: its table, with the
// path of this program and of the tool it drives filled in, then advice.
type Install struct {
	// Tool is the program this integration drives, looked up on PATH here
	// because install runs in your shell and nothing that runs later does
	// (D-67). Empty when there is none.
	Tool string
	// Table is the table to file, given this program's absolute path and the
	// tool's, which is "" when the tool was not found.
	Table func(program, tool string) string
	// Advice follows on stderr: what only the user can decide.
	Advice string
}

// Install writes the drop-in and says where, on stdout; the advice goes to
// stderr. It takes no options. The exit code is 1 when the tool was not
// found, and the table says so too.
func (i Integration) Install(install Install, out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems, "%s install takes no options.\n", i.Name)
		return 2
	}
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", i.Name, err)
		return 1
	}

	tool, lookup := "", error(nil)
	if install.Tool != "" {
		tool, lookup = exec.LookPath(install.Tool)
	}
	written, err := i.WriteDropIn(install.Table(program, tool))
	if err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", i.Name, err)
		return 1
	}
	fmt.Fprintf(out, "wrote %s\n", written)
	fmt.Fprint(problems, install.Advice)
	if lookup != nil {
		fmt.Fprintf(problems, "\n%s is not on this PATH, so the `%s` line in that file is empty: fill it\n"+
			"in with wherever %s actually is.\n", install.Tool, install.Tool, install.Tool)
		return 1
	}
	return 0
}

// Uninstall removes the drop-in and says so.
func (i Integration) Uninstall(out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems, "%s uninstall takes no options.\n", i.Name)
		return 2
	}
	layout, err := i.layout()
	if err != nil {
		fmt.Fprintf(problems, "%s uninstall: %v\n", i.Name, err)
		return 1
	}
	if err := config.RemoveDropIn(layout, i.Name); err != nil {
		fmt.Fprintf(problems, "%s uninstall: %v\n", i.Name, err)
		return 1
	}
	fmt.Fprintf(out, "removed %s\n", layout.DropIn(i.Name))
	return 0
}
