# Installation

Agreed 2026-09-29, from the review of the install path of the day before.
Recorded as D-85 in plan.md. Two commands set a machine up: `make install` at
the root builds every module, then `agent-notify install` sets up every
integration on PATH. Each integration writes what only it can know into a file
it owns. `config.toml` is never written by any program, and `[container]
order` stays the one fact only the user knows.

Every step leaves all eight modules green.

1. **Drop-ins.** Core reads `conf.d/*.toml` beside `config.toml`, sorted, then
   `config.toml` on top: what the user writes wins, and `enabled = false` in
   their file still switches an integration off. A drop-in that does not
   parse is reported by name and skipped; the user's file being refused is
   still what `ErrRefused` means. `[integration.<name>]` gains `launch-agent`,
   the launchd label that keeps a resident display running, so `doctor` can
   ask launchd about it.
2. **The SDK writes the drop-in.** `Integration.Install` writes
   `conf.d/<name>.toml` with the table it used to print and says where it
   wrote it; the advice still goes to stderr. `Integration.Uninstall` removes
   it. `hook.WriteDropIn` and `hook.RemoveDropIn` are the same two for an
   agent-integration. Both go through one writer in `internal/config`.
3. **A resident display installs itself completely.** `InstallBundle` builds
   the `.app`, writes the drop-in with its `sign` and `launch-agent`, writes
   the plist into `~/Library/LaunchAgents`, and reloads the job, so re-running
   install after a rebuild is the whole upgrade. `UninstallBundle` reverses
   every step. The reload waits for the old job to finish going away:
   `bootout` returns before launchd is done, and a `bootstrap` inside that
   window fails with "Input/output error" and leaves nothing running.
4. **Agent-integrations file their own table.** `agent-notify-claude install`
   writes `[agent.claude] binary = "claude"` into its drop-in beside the hooks
   it already writes; codex the same. `uninstall` removes the hooks and the
   drop-in. `hook.Main` takes the program's own subcommands as a map, like
   `subscribe.Main`.
5. **Core files `agent-notify-binary`.** `agent-notify install` writes
   `conf.d/agent-notify.toml` naming its own executable before it hands over
   to anybody, so the hook and every launchd child find core without a PATH.
6. **One dispatcher fans out.** `agent-notify install` with no name runs
   `agent-notify-<name> install` for every program on PATH; with names, those.
   `agent-notify uninstall` is the reverse. Both run the integration as a
   child rather than replacing the process, so that several can run in turn.
7. **`go.work` and a `Makefile`.** `make install` builds all eight modules into
   `$(go env GOBIN)`; `make test` runs every module's tests. The workspace also
   makes `go test ./...` from the root cover everything.
8. **`doctor` checks what goes wrong first.** Whether `agent-notify-binary`
   resolves; what `containers.Configured` complains about; an agent with
   sessions in the store and no `[agent.<name>]` table; and whether each
   resident display's launch agent is loaded.
9. **Record.** D-85 in plan.md amending D-66, §A14, §A12 and the launch-agent
   paragraph of D-52; the core README's installation and configuration
   sections; each integration README's install section; `todo.md`.

What stays manual: the picker's keybinding in zellij's KDL config, which is
printed, and macOS's notification permission, which is asked.
