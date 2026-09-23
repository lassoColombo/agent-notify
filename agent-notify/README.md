# agent-notify

A semaphore and dashboard substrate for coding agents: it watches every agent
running on your machine and tells whatever you look at — a status bar, a
multiplexer's tab titles, a notification, a picker — what each one is doing, in
a vocabulary that does not depend on which agent it is or which tool is looking.

This is the **core** repository: one Go module, a library and a CLI. The agents
and the tools live in their own repositories, one each.

**[plan.md](plan.md) is the design and the plan.** Section A is what this is and
the rules it keeps; section B is the meta-plan being followed. Section B is also
the honest answer to "how much of this works": it has been the one running on
the machine it was written on since 2026-09-18, and through M15 it tracks real Claude
and codex sessions in one vocabulary, notices when one dies, paints them onto
zellij's pane and tab titles, puts the semaphore on your menu bar, takes you to
any of them through every layer it is buried under — window, then multiplexer,
then pane — and starts and supervises the integrations that do it.

## What runs today

```
agent-notify list [--json] [--all]    what is running, most urgent first
agent-notify tail [--json]            watch it change, live
agent-notify install <integration>    hand over to that integration to set itself up
agent-notify focus-session <session>  bring it to the front, through every container
agent-notify focused <session>        are you already looking at it? yes/no/cannot-tell
agent-notify annotate <session> <owner> <json>
agent-notify watcher <run|start|stop|restart|reload|status>
agent-notify record | replay <file>   capture a session's changes, play them into a display
agent-notify doctor                   where everything resolves to, and what is wrong with it
agent-notify version                  what every integration handshakes against
agent-notify completion <shell>       a completion script, and see below
```

`agent-notify <command> --help` says the rest. Every command carries its own
description and its own flags, so there is no second place where the two can
disagree.

`doctor` names what it does not yet check, so its output never reads as a clean
bill of health for something still being built.

## Completion

The command line is [cobra](https://github.com/spf13/cobra), and the reason is
one feature rather than the family of them: **it can answer with what is
actually happening.**

```
$ agent-notify focus-session <TAB>
lenny-dmilog-profili    finished-a-turn
agent-notify-go-design  working
```

Those are the sessions that are running, most urgent first, each glossed with
what it is doing — asked of the program at the moment you press TAB, with no
cache and no spec to go stale. `install <TAB>` offers the integrations that are
on your PATH, which is the same question `install` asks a moment later, so it
cannot offer something that would then be refused. `--wake-on <TAB>` offers the
record fields.

```
agent-notify completion zsh  > "${fpath[1]}/_agent-notify"
agent-notify completion bash > /etc/bash_completion.d/agent-notify
agent-notify completion fish > ~/.config/fish/completions/agent-notify.fish
```

**nushell** has no generated script and does not need one: it takes an external
completer, and cobra's `__complete` is a protocol any of them can speak — for
every cobra program you have, not just this one. Flags come along for free:
`--<TAB>` lists them with their help text, `--output=<TAB>` lists that flag's
values.

The part worth getting right is the `:N` line cobra closes with. It is a
bitfield saying what to do with the answer, and dropping it is the difference
between `kubectl apply -f <TAB>` offering your yaml files and offering you the
word "yaml".

```nu
# completers/cobra.nu — one export, and it is the completer itself, so no config
# has to write a wrapper closure of its own. A command handing out a closure
# rather than an exported value, because a closure is not a parse-time constant
# and `export const` will not hold one.
export def cobra-completer []: nothing -> closure {
  {|spans: list<string>|
    let answer = (do -i { ^($spans | first) __complete ...($spans | skip 1) } | complete)
    if $answer.exit_code != 0 { return null }

    # Nothing obliges a program to sign its answer, so the last line is checked
    # for the directive rather than assumed to be it.
    let lines = ($answer.stdout | lines)
    let signed = (($lines | is-not-empty) and (($lines | last) =~ '^:[0-9]+$'))
    let directive = (if $signed { $lines | last | str substring 1.. | into int } else { 0 })
    let offered = (if $signed { $lines | drop 1 } else { $lines }
      | where {|line| $line | str trim | is-not-empty })

    # 1 is ERROR. null hands the slot back to nushell; an empty list would
    # instead assert that nothing exists, and stop it trying anything else.
    if ($directive bit-and 1) != 0 { return null }

    # 8 is FILTER_FILE_EXT, 16 is FILTER_DIRS: the lines are not candidates at
    # all, they are a filter over the filesystem, and the completing is ours.
    if ($directive bit-and 24) != 0 {
      let partial = ($spans | last)
      let prefix = ($partial | str replace -r '[^/]*$' '')
      let dirs_only = (($directive bit-and 16) != 0)
      let hidden_too = ($partial | split row '/' | last | str starts-with '.')
      return (do -i { ls -a (if ($prefix | is-empty) { "." } else { $prefix | path expand }) } | default []
        | where {|e| $hidden_too or (not ($e.name | path basename | str starts-with '.')) }
        # Directories always survive: you cannot reach the file you want
        # without descending to it first.
        | where {|e| $e.type == "dir" or ((not $dirs_only) and (($e.name | path parse | get extension) in $offered)) }
        | each {|e| $prefix + ($e.name | path basename) + (if $e.type == "dir" { "/" } else { "" }) })
    }

    # 4 is NO_FILE_COMP. Without it, an empty answer means "I have nothing, let
    # the shell try files" — so it has to be null, not an empty list.
    if ($offered | is-empty) {
      return (if ($directive bit-and 4) != 0 { [] } else { null })
    }

    # 32 is KEEP_ORDER, and there is nothing to do: nushell leaves an external
    # completer's order alone under every algorithm and sort setting.
    $offered | each {|line|
      let halves = ($line | split row "\t")
      {value: ($halves | first), description: ($halves | skip 1 | str join " ")}
    }
  }
}
```

Then one arm covers every cobra program you name, with nothing per-program to
write:

```nu
use completers/cobra.nu *

match $spans.0 {
  agent-notify => (cobra-completer)
  _ => $carapace_completer
} | do $in $spans
```

## The integrations

An agent-integration turns one agent's hooks into this vocabulary; a
tool-integration renders it or acts on it. Each lives in its own repository and
links this one as a library — one per agent, and one per (tool, role), so a tool
that both draws and navigates is two programs you install separately.

| | role | |
| --- | --- | --- |
| [agent-notify-claude](../agent-integrations/agent-notify-claude) | agent | Claude Code's hooks |
| [agent-notify-codex](../agent-integrations/agent-notify-codex) | agent | Codex CLI's hooks |
| [agent-notify-macos-bar](../tool-integrations/agent-notify-macos-bar) | display | the semaphore on the macOS menu bar |
| [agent-notify-macos-notifications](../tool-integrations/agent-notify-macos-notifications) | display | a macOS notification when an agent wants you |
| [agent-notify-zellij-display](../tool-integrations/agent-notify-zellij-display) | display | pane and tab titles |
| [agent-notify-sketchybar](../tool-integrations/agent-notify-sketchybar) | display | counters and chips on the menu bar |
| [agent-notify-picker](../tool-integrations/agent-notify-picker) | display | every session in a terminal, and a way into one |
| [agent-notify-zellij-container](../tool-integrations/agent-notify-zellij-container) | container | placing and focusing a session |
| [agent-notify-aerospace-container](../tool-integrations/agent-notify-aerospace-container) | container | finding and raising the window a session is in |

**Several displays at once is the ordinary case**, not a clever one. They never
talk to each other: each connects to the watcher on its own, is handed the same
state, and renders it wherever it renders. The two macOS ones are the clearest
example — the bar draws a semaphore, the notifications interrupt you — and they
are separate repositories, separate binaries, separate config tables and
separate installs, because the menu bar and Notification Centre are two surfaces
that happen to belong to one operating system.

Writing one is `hook.Record(report)` for an agent, or a render function and a
ten-line `main` for a tool. Neither ever sees the protocol, the store layout or
the socket.

One thing cuts across the roles. Anything that needs to know **where** a session
is — which pane, which window — has to read that out of the agent's own
environment, and only a child of the agent's hook can. So it answers
`capture-environment`, whatever else it is: a container does it alongside its
other three subcommands, and a display adds one case to its `main`. That is the
`capture` package, and it is the only way anything gets out of an agent's
environment.

## Configuration

`~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/`. There
is no file by default and running without one is the supported case. A file that
cannot be understood is never fatal: the complaint goes to the log and the
defaults apply.

**One table per integration, and each reads only its own.** An integration is
installed by adding its table and uninstalled by deleting it; nothing else in
the file changes, and no integration can be made to depend on another being
present:

```toml
[integration.macos-bar]
binary = "/Users/you/Applications/agent-notify-macos-bar.app/Contents/MacOS/agent-notify-macos-bar"

[integration.macos-bar.settings]
rows = 8

[integration.macos-notifications]
binary = "/Users/you/Applications/agent-notify-macos-notifications.app/Contents/MacOS/agent-notify-macos-notifications"

[integration.macos-notifications.settings]
sign = "agent-notify self-signed"
```

Everything under `[integration.<name>.settings]` belongs to that integration
alone, and a key it never declared is refused **by name** — because a misspelled
setting that changes nothing and says nothing is the config bug people give up
on.

**Nothing in this file is an integration's own knowledge.** Everything here is
something only you can decide: which agents you run, where their binaries are,
which tools you want, how long to remember an ended session, what a state looks
like. A key whose correct value is a fact about a program does not belong here —
it would only be a second copy that can disagree with the first, and you would
be the one who broke it. A table once listed the environment variables an
integration reads; it does not any more, because the integration already knew.

Set `AGENT_NOTIFY_ROOT` to move the state directory, the runtime directory and
the configuration file beneath it — one variable, so that running an isolated
instance is one step.

## Building

Go 1.26 or newer. If your shell exported `GOROOT` from an outer context it will
override the toolchain pinned in `.tool-versions`; `env -u GOROOT go build ./...`
is the fix.
