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
// and nothing follows what we add. Safe, that is, unless `hooks` is already
// written another way — inline, or as a plain table of arrays — and then the
// two cannot both be TOML; so what would be written is parsed first, and a file
// it would break is left as it is.
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

	var parsed map[string]any
	if len(existing) > 0 {
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
			return 0
		}
	}

	addition := block
	if len(existing) > 0 {
		if !strings.HasSuffix(string(existing), "\n") {
			addition = "\n" + addition
		}
		addition = "\n" + addition
	}
	written := string(existing) + addition
	if err := toml.Unmarshal([]byte(written), &parsed); err != nil {
		fmt.Fprintf(os.Stderr,
			"agent-notify-codex install: %s already says something about `hooks` that the block cannot be\n"+
				"added beside, so nothing was changed; `--print` shows the block to merge by hand:\n%v\n",
			*path, err)
		return 1
	}
	if err := replace(*path, written); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-codex install: %v\n", err)
		return 1
	}

	fmt.Printf("added %d hook(s) to %s.\n", len(SubscribedHooks), *path)
	fmt.Fprintln(os.Stderr, "\nCodex will ask you to trust these the first time it runs one, and will")
	fmt.Fprintln(os.Stderr, "record the answer in that same file. That step is yours: the trust hash is")
	fmt.Fprintln(os.Stderr, "codex's own and writing one here would be forging your consent to run a")
	fmt.Fprintln(os.Stderr, "program on every hook.")
	return 0
}

// uninstall takes the block install appended out again, verbatim. A block somebody has edited is not recognised and is left, with
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
		if err := replace(*path, cleaned); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify-codex uninstall: %v\n", err)
			return 1
		}
		fmt.Printf("removed %d hook(s) from %s.\n", len(SubscribedHooks), *path)
	}
	return 0
}

// replace puts content where path is, all of it or none of it: a config cut
// short by an interrupted write is a codex that does not start. A symbolic
// link is followed, so a config kept with somebody's dotfiles stays there, and
// the mode the file had is the mode it keeps.
func replace(path, content string) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	temporary := path + ".writing"
	if err := os.WriteFile(temporary, []byte(content), mode); err != nil {
		return fmt.Errorf("cannot write %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("cannot put %s in place: %w", path, err)
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
