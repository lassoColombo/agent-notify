package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/pelletier/go-toml/v2"
)

// install writes this program into codex's own config.toml, which is how a hook
// comes to exist at all.
//
// It APPENDS. A config file is a thing a person wrote, with their model, their
// sandbox policy, their MCP servers and their comments in it, and round-tripping
// all of that through a TOML encoder to add seven blocks would hand it back
// stripped of the comments and reordered. Appending at the end of the file is
// safe for one reason worth stating: a table header ends the table before it,
// and nothing follows what we add.
//
// It is idempotent: a file that already names this program anywhere in its
// hooks is left alone. There is no merge and no update of a stale path — the
// hooks table is a place people put their own commands, and rearranging it
// under them would be a worse failure than saying what to change.
func install(arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	print := flags.Bool("print", false, "show what would be added and change nothing")
	path := flags.String("config", defaultConfig(), "codex's config file")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: %v\n", err)
		return 2
	}

	program, err := os.Executable()
	if err != nil {
		// A hook's PATH is not your shell's PATH, so the absolute path is what
		// gets written. Without one there is nothing useful to write.
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: cannot find my own path: %v\n", err)
		return 1
	}
	block := hookBlock(program)

	if *print {
		fmt.Print(block)
		fmt.Print(agentTable)
		return 0
	}

	existing, err := os.ReadFile(*path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		existing = nil
	case err != nil:
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: cannot read %s: %v\n", *path, err)
		return 1
	}

	if len(existing) > 0 {
		var parsed map[string]any
		if err := toml.Unmarshal(existing, &parsed); err != nil {
			// Refusing is the point: appending to a file that does not parse
			// turns one problem into two, and the second one looks like ours.
			fmt.Fprintf(os.Stderr,
				"agent-notify-codex install: %s does not parse as TOML, so nothing was changed:\n%v\n",
				*path, err)
			return 1
		}
		if strings.Contains(string(existing), filepath.Base(program)) {
			fmt.Printf("%s already runs %s; nothing changed.\n", *path, filepath.Base(program))
			return fileTheAgentTable()
		}
	}

	if err := os.MkdirAll(filepath.Dir(*path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: cannot create %s: %v\n", filepath.Dir(*path), err)
		return 1
	}
	addition := block
	if len(existing) > 0 {
		if !strings.HasSuffix(string(existing), "\n") {
			addition = "\n" + addition
		}
		addition = "\n" + addition
	}
	if err := appendTo(*path, addition); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: %v\n", err)
		return 1
	}

	fmt.Printf("added %d hook(s) to %s.\n", len(SubscribedHooks), *path)
	fmt.Fprintln(os.Stderr, "\nCodex will ask you to trust these the first time it runs one, and will")
	fmt.Fprintln(os.Stderr, "record the answer in that same file. That step is yours: the trust hash is")
	fmt.Fprintln(os.Stderr, "codex's own and writing one here would be forging your consent to run a")
	fmt.Fprintln(os.Stderr, "program on every hook.")
	return fileTheAgentTable()
}

// agentTable is what core needs to recognise codex's process in a hook's
// ancestry (§A8.4). The name is codex's, so it is this program's to file.
const agentTable = "# Written by `agent-notify-codex install`.\n[agent." + AgentName + "]\nbinary = \"codex\"\n"

func fileTheAgentTable() int {
	written, err := hook.WriteDropIn(AgentName, agentTable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", written)
	return 0
}

// uninstall takes the block install appended out again, verbatim, and the
// table away. A block somebody has edited is not recognised and is left, with
// a word about it: rearranging a hooks table under a person is worse than
// asking them to.
func uninstall(arguments []string) int {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", defaultConfig(), "codex's config file")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: %v\n", err)
		return 2
	}
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: cannot find my own path: %v\n", err)
		return 1
	}

	existing, err := os.ReadFile(*path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fmt.Printf("%s does not exist; nothing to remove.\n", *path)
	case err != nil:
		fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: cannot read %s: %v\n", *path, err)
		return 1
	case !strings.Contains(string(existing), hookBlock(program)):
		if strings.Contains(string(existing), filepath.Base(program)) {
			fmt.Fprintf(os.Stderr, "%s names %s in a block this program did not write as it is, so\n"+
				"nothing there was changed: take the [[hooks.*]] entries naming it out yourself.\n",
				*path, filepath.Base(program))
		} else {
			fmt.Printf("%s does not run %s; nothing to remove.\n", *path, filepath.Base(program))
		}
	default:
		cleaned := strings.Replace(string(existing), hookBlock(program), "", 1)
		cleaned = strings.TrimRight(cleaned, "\n") + "\n"
		if err := os.WriteFile(*path, []byte(cleaned), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: cannot write %s: %v\n", *path, err)
			return 1
		}
		fmt.Printf("removed %d hook(s) from %s.\n", len(SubscribedHooks), *path)
	}
	if err := hook.RemoveDropIn(AgentName); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: %v\n", err)
		return 1
	}
	fmt.Println("removed the [agent." + AgentName + "] table")
	return 0
}

func appendTo(path, text string) error {
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if _, err := handle.WriteString(text); err != nil {
		handle.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// hookBlock is what gets added: one array entry per hook, all running the same
// command.
//
// The same command for all seven, because codex spells the event into the
// payload — claude's equivalent has to name one per line and can therefore
// disagree with itself.
func hookBlock(program string) string {
	var out strings.Builder
	out.WriteString("# agent-notify: tells agent-notify what this codex session is doing.\n")
	out.WriteString("# It never writes to stdout and never exits non-zero, so it cannot\n")
	out.WriteString("# block a tool, decide a permission request, or fail a turn.\n")
	for _, hook := range SubscribedHooks {
		fmt.Fprintf(&out, "\n[[hooks.%s]]\n[[hooks.%s.hooks]]\ntype = \"command\"\ncommand = %q\n",
			hook, hook, program)
	}
	return out.String()
}

// defaultConfig is where codex keeps its configuration. CODEX_HOME is codex's
// own override and is honoured for the same reason AGENT_NOTIFY_ROOT is: it is
// how a person runs an isolated instance.
func defaultConfig() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
}
