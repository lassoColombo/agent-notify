# todo

Opened 2026-09-23, the day the ten repositories became one. Everything here is
either a defect verified against the source or a consequence of the monorepo
move. Each item says what is actually wrong, where, and what has to be decided
before it can be closed.

## Decisions nobody but you can make

- **Two accounts of the macOS menu bar still disagree, and the machine is on
  the older one.** `install` now writes a launch agent naming the binary
  INSIDE the bundle and loads it (D-85). On 2026-09-29 that was installed and
  then repointed by hand at the bare `/opt/homebrew/bin/agent-notify-macos-bar`,
  because the `[measured 2026-09-21]` note said a bundled build is put up,
  reports visible at level 25 with a real frame, and is never drawn — and
  nothing could verify otherwise: the accessibility API was not granted, and
  `check` says being ON the bar is not being DRAWN on it anyway.
  - D-52 (`plan.md:3195`) argues the bundle is **mandatory** for the opposite
    reason: no bundle means no surviving preferences domain, so `autosaveName`
    has nowhere to file the position, the item lands leftmost, and a full menu
    bar does not draw the leftmost slot.
  - D-52's third bullet also says the bundle "is a wrapper, not a copy… a
    symlink to the real binary". `bundle.go:25-40` says it is a **copy**, and
    explains why the symlink was abandoned: `codesign -s - --force` refuses one
    and an unsigned bundle is invisible to the notification system. So D-52 is
    stale on the mechanism as well as the conclusion.
  - The 2026-09-21 measurement is **not in plan.md** at all. That day produced
    D-68 through D-74 and none of them touch the menu bar.
  **The decision**: which measurement still holds. Read together they point at
  signature trust rather than bundle-versus-bare, which would make a trusted
  signature the fix and the bare binary a workaround. **The test is one
  command and a glance**: `agent-notify install macos-bar` puts the bundle
  back, and you look at the menu bar. Once you know, D-52 needs an amendment,
  the finding needs to leave `config.toml` and enter plan.md, and — if the
  bundle wins — the by-hand plist in `config.toml`'s comment goes.

## Monorepo follow-through

- **D-37 says "one repository per (tool, role)" and that is now false.**
  `plan.md:2869` ("a tool that genuinely is one thing is still exactly one
  repository"), `plan.md:3292` ("one repository per (tool, role)"),
  `plan.md:3304` ("a test in each repository"), `plan.md:1552`, `plan.md:4844`.
  The substance of D-37 survives the move untouched — each integration is still
  its own Go module, its own config table, its own handshake name, and still
  genuinely separable. Only the unit changed. This wants a recorded amendment
  saying the unit is now the module, not a silent edit of a decision already
  taken.

- **All nine integration `go.mod` files still describe themselves as separate
  repositories.** Each carries the comment "Core is not published yet. This line
  comes out in M18, when it is; until then an integration is built beside it and
  a clone of this repo alone does not compile, which is a thing M18 exists to
  fix." In a monorepo the last clause is simply wrong: cloning it compiles
  everything. The `replace` lines themselves stay until M18.

- **Ten READMEs disagree about where to clone from, because there is no
  remote.** Four carry a placeholder — `agent-notify/README.md:155`
  (`<your remote>/agent-notify.git`),
  `tool-integrations/agent-notify-macos-notifications/README.md:182` and
  `tool-integrations/agent-notify-zellij-display/README.md:102`
  (`<the agent-notify monorepo>`),
  The other five guessed `git@github.com:lassoColombo/agent-notify.git` by
  analogy with the old repos. One real URL closes all nine.

- **There is no `go.work`, so `go build ./...` from the root does nothing.** Ten
  modules, ten separate builds. A workspace file would make the root buildable
  and testable in one command; it also changes how the `replace` lines behave,
  so it is worth doing deliberately rather than by reflex.

## Housekeeping

- **An agent committed, and the commit is still there.** `be3a2fb feat: refactor
  into monorepo` holds the pre-README tree; everything since is unstaged.
  `git update-ref -d HEAD` drops it and touches no file. Decide whether to keep
  it as a baseline or unwind it.

- **The only copy of ten repositories' history is in a session scratchpad.**
  `…/scratchpad/git-backup/*.git.tgz`, 9.7 MB, ten archives, one per deleted
  `.git`. Nothing was ever pushed anywhere, so when that directory is cleaned
  the history is gone for good. Move it somewhere durable or decide out loud
  that it is not wanted.

## Closed 2026-09-29

- **Installation** (D-85): integrations file what only they know in
  `conf.d/<name>.toml`; `config.toml` is never written by a program. `make
  install` builds every module, `agent-notify install` sets every integration
  on PATH up and `uninstall` takes it back; agent-integrations file their
  `[agent.<name>]` table, core files `agent-notify-binary`, the macOS displays
  write and load their launch agents, and `doctor` checks the binary, the
  containers, the agent tables and launchd. [install-plan.md](install-plan.md)
  is the working plan. The `go.work` item under "Monorepo follow-through" is
  closed with it: workspace mode does not make `./...` span modules, so the
  Makefile walks them.

- **It was installed on this machine the same day.** `make install
  GOBIN=/opt/homebrew/bin`, then `agent-notify install`. The three stale
  binaries — sketchybar, zellij-display, zellij-container — were moved to
  `~/.config/agent-notify/backup-before-d85/stale-binaries/`, and the old
  `config.toml` and launch agents are beside them. `config.toml` is now only
  `[container] order`, `sound = false` and the menu-bar note; everything else
  it used to carry is in `conf.d/`. Two bugs came out of doing it:
  `InstallBundle` bootstrapped launchd before the old job had finished going
  away, which left the notifications display stopped, and `make install`
  reported `$(go env GOPATH)/bin` rather than where it had actually put
  anything. Both are fixed.

## Closed 2026-09-28

- **Simplification** (D-84): the eighteen findings of the same day's review
  under "simpler logic" and "easier to maintain". One world for every reader,
  one shape for capabilities, an empty capture stored, `Core` opened once and
  kept, the daemon split from its lock and its store watch, the floor table
  gone, the socket-era prose gone, and `Install`, `InstallBundle`, `hook.Main`,
  `hook.LastResponses` and `FieldsRenderedButNotWokenFor` in core with the
  seven copies deleted. [simplification-plan.md](simplification-plan.md) is
  the working plan. Two core tests, the handshake report and the tool refusal,
  time out under a full parallel `go test ./...` on a loaded machine and pass
  alone; they did before this too.
- **The store is the bus** (D-83): both sockets, the `internal/subscriber`
  client, `session/protocol.go`, `record`/`replay` and `watcher status` are
  gone; the session-watcher and the resident displays wake on a kqueue over
  `state/sessions` and `state/ended`. One SDK, `subscribe.Main`, for every
  tool-integration; `bundle.go` and the tool-path check are in core; the hook
  asks the config, not the report, who to run; the `Exits` map has its mutex;
  claude reads the event from `hook_event_name`; `hook.ReadBackwards` replaces
  three copies of the reverse reader; zellij is one program.
  [store-as-bus-plan.md](store-as-bus-plan.md) is the working plan.

## Closed 2026-09-27

- **Every duration a person writes now reads the way people write durations**
  (D-78). `agentnotify.Duration` is exported from the root package and imported
  by both bars rather than reinvented: `keep-ended-sessions = "168h"`,
  `announce = "8s"`, `announce = "0s"` to turn one off. A struct rather than a
  named `time.Duration`, because go-toml decodes a bare number natively into
  anything integer-kinded and that is how `announce = 8` came to mean eight
  nanoseconds. Text that is not a duration is now a named complaint that costs
  the value and not the file, which is what `subscribe/settings.go` already
  promised in a comment.
- **A bug came out with it.** Sketchybar's `announce = 0` was documented and
  commented as "the bar stays passive" and was indistinguishable from an absent
  key, so it silently took the eight-second default. The setting is a pointer
  now, so absent and `"0s"` are different answers.
- Q16 reopened and answered yes; D-62's examples in plan.md updated; the three
  READMEs rewritten.
- **agent-notify-sketchybar was removed** (D-79), deprecated by deletion rather
  than by a banner: unmaintained and still in the tree meant keeping it
  compiling against a core that breaks its own API freely, and three of its
  tests drove a live sketchybar and failed on every full sweep. The monorepo is
  nine modules and eight integrations now, and the whole suite is green. The
  rule is in `CLAUDE.md`; the code is in `827ccc4` and before.

## Closed 2026-09-23

- The ten repositories became one; `.git` removed from each, `git init` at the
  root, per-project `.gitignore` files left in place (their root-anchored
  patterns anchor to their own directory, so they still match).
- All ten READMEs rewritten from the source, installation and configuration
  carrying the weight.
- **The usage text of five tool-integrations predated D-66.** Every one claimed
  `install` *adds* the table to the config, which D-66 reversed, and four also
  advertised an `install --print` that no flagset defines. All five now describe
  what the program does. The agent-integrations keep their real `--print`; D-66
  carves them out explicitly.
- **`install` now honours `CLAUDE_CONFIG_DIR`** (`agent-notify-claude/install.go:71`),
  the same way `whereClaudeKeepsItsLiveSessions` always has. Verified against a
  sandboxed `HOME` in both directions.
- **`mapping.go:2` said "eight events"** where core declares nine and this
  program emits seven. It now states its own output.
