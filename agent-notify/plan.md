# agent-notify

A semaphore and dashboard substrate for coding agents.

This is the living plan. It has two parts: **[A] Design** — the goal, the
intentions, the architecture and the rules, all of it global and slow-moving —
and **[B] Plan** — the meta-plan we are executing, expanded step by step as we
learn what a step actually contains.

Every claim in Design is marked:

| Marker | Meaning |
| --- | --- |
| **[decided]** | agreed in discussion; change it only by amending this document and logging the change in §A19 |
| **[open]** | known to be undecided; carries a question number from §A18 |
| **[verified]** | checked against a real machine; the check is named so it can be repeated |
| **[assumed]** | believed true, not yet checked; verifying it is a plan step |
| **[deferred]** | deliberately postponed to a named future discussion |
| **[proposed]** | worked out in discussion and written down so it is not lost, but **not yet agreed**; every one of these becomes decided, or changes, or dies |

Last amended: 2026-09-17 (payload and integration sweep).

---

# A. Design

## A1. What this is, and what it promises

agent-notify watches every coding agent running on your machine and tells
whatever you look at — a status bar, a multiplexer's tab titles, a notification,
a picker — what each of them is doing right now, in a vocabulary that does not
depend on which agent it is or which tool is doing the looking.

Four promises, and every rule in this document exists to keep one of them:

1. **Run several agents at once and they all show up in one place.** Claude,
   Codex, Cursor and whatever comes next report into the same store and reduce
   to the same states, so a bar can count them together and order them by
   urgency without knowing what any of them is.
2. **Switch agent and nothing on the display side changes.** Adding an agent is
   writing one small program that translates that agent's hooks. No display, no
   container and no part of core is touched.
3. **Switch tool and nothing else changes.** Move from zellij to tmux, from
   sketchybar to the macOS menu bar: swap one tool-integration. The store, the
   session-watcher, the agents and every other integration are untouched.
4. **Build your own.** A display is a small Go program that implements one
   render function, or any program in any language that can read a socket. A
   container is often four lines of TOML.

**Non-goals for v1**, stated so they stop being questions:

- Agents running on another machine (over ssh, in a container, in CI). The
  session identity reserves a host field and liveness reserves a lease mechanism
  so this stays possible, but nothing implements it.
- Windows. The design is unix-only by construction: unix sockets, process
  ancestry, `flock`, `kqueue`/`pidfd`.
- Multi-user or system-wide operation. Everything is per-user, under the user's
  own directories, readable by nobody else.
- Driving the agent. We observe agents and we move *your attention* to them. We
  never type into one, answer a prompt, or kill one.
- **Analysis across sessions and across time.** A session carries a little
  history of its own — its recent messages and its recent state changes —
  because displays need it, and §A7.7 is precise about what that is. What we do
  not do is keep a record of sessions after they are gone, or compute over one:
  no "three hours lost to permission prompts this week". **[decided 2026-09-17,
  D-10]**; §A7.7 records what it would take to change that.

## A2. Glossary

Words used in exactly one sense throughout this document.

| Term | Meaning |
| --- | --- |
| **agent** | a coding agent CLI that runs sessions and fires hooks: `claude`, `codex`, `cursor`, `gemini`. |
| **session** | one run of one agent, identified by the id that agent assigns it. Survives being resumed. |
| **agent-integration** | the small program an agent execs as a hook. One per agent, one repo each. |
| **hook event** | what an agent-integration reports to core: what happened, plus a detail label, plus captured context. |
| **record-agent-event** | the core entry point that receives a hook event, updates the store and pokes the session-watcher. Runs *inside the hook process*. |
| **session-watcher** | the single long-lived core process per user. Watches for death, fans out, supervises integrations. |
| **session record** | the stored description of one session. |
| **session store** | the on-disk set of session records. The only source of truth. |
| **kernel state** | one of a small, closed, core-owned set of states, defined by what it demands of the human. |
| **detail** | a free-form, agent-chosen refinement of a kernel state. Core never interprets it. |
| **tool-integration** | a program that integrates one external tool. Declares itself a display, a container, or both. |
| **display** | a tool-integration that renders session state somewhere a human looks. |
| **container** | a tool-integration that knows where a session physically lives and can bring it to the front. |
| **container-coordinates** | one container's coordinates for one session: a zellij pane id, an aerospace window id. A pane id means nothing except as a coordinate inside zellij, which is why the container names the space. |
| **captured-context** | the environment variables and process ancestry record-agent-event reads inside the hook process, because that is the only place they exist. |
| **urgency-rank** | the kernel state's position in the urgency order, carried beside its name so an unfamiliar state still sorts and still renders. |
| **annotation** | a section of a record written by somebody who is not the agent, under a key naming its owner. **container-coordinates** are an annotation written by a container. |
| **enricher** | an integration that annotates records without displaying anything and without placing anything — a git branch, a project name, a task id. |
| **subscriber** | anything holding a connection to the session-watcher: a display, a notifier, a `wait` command, a menu-bar app. Not all subscribers render. |
| **session history** | the last few messages and state changes of one session, bounded by count and deleted with it. Content for a display, not a record of the past (§A7.7). |
| **ambient session** | the session a command is running inside, resolved from its own process ancestry rather than from an argument. |

### A2.1 Names, and where the house rule stops

**[decided 2026-09-17 — D-22]** A name says what the thing is; a bare verb and a
bare abstract noun are names that have not finished. That rule governs Go
identifiers and this document. It stops at two borders, deliberately.

| The thing | In Go and here | On the wire | At the command line |
| --- | --- | --- | --- |
| the entry point an agent-integration calls when its agent did something | record-agent-event | — | `agent-notify report-event` |
| the single long-lived process | session-watcher | — | `agent-notify watcher` |
| one container's coordinates for a session | container-coordinates | `annotations.<container>` | — |
| what the hook could read and nothing else could | captured-context | `captured_context` | — |
| a kernel state's position in the urgency order | urgency-rank | `rank` | — |
| the agent-chosen refinement of a state | state-detail | `detail` | — |

- **The wire format is not Go.** It is read by shell scripts and by programs in
  other languages, and a person writing `jq .rank` is served by a short key. The
  keys still say what they are; they simply do not carry the qualifier that Go
  needs because Go has no surrounding pair to disambiguate them.
- **The command line is typed by people.** `agent-notify report-event` over
  `agent-notify record-agent-event` buys nothing but keystrokes.
- **`detail` keeps its short form in prose as well**, because it never appears
  alone: it is always the second half of the kernel-and-detail pair, and the
  pair supplies exactly the qualifier the rule asks for. `StateDetail` in Go,
  where there is no pair in sight.

## A3. Architecture

### A3.1 Three families of repository

**[decided]** Every leaf below is an **independent repository**, with its own
`.git`, its own `.tool-versions`, its own dotfiles, its own release cycle and
its own version number. Nothing is a submodule and nothing is a Go workspace.

**[amended 2026-09-23, and again 2026-09-29 — D-85]** The leaves became
directories of one repository, and the last sentence went with them: there is a
`go.work` listing all eight modules, for an editor that wants to see them at
once. The `replace` directives are still what say where core is.

```
~/projects/personal/agent-notify/         a directory on one machine, NOT a repo
    agent-notify/                         REPO: the core. One Go module: library + CLI.
        plan.md                           this document
    agent-integrations/                   a plain containing directory, NOT a repo
        agent-notify-claude/              REPO: one per agent
        agent-notify-codex/               REPO
    tool-integrations/                    a plain containing directory, NOT a repo
        agent-notify-zellij-display/      REPO: one per (tool, role)
        agent-notify-zellij-container/    REPO
        agent-notify-sketchybar/          REPO
        agent-notify-aerospace-container/ REPO
```

**[decided 2026-09-18 — D-37] One repository per (tool, role), not per tool.**
zellij plays two roles and is therefore two programs, installed independently.
The reasons are in D-37; the consequence here is that "one per tool" was the
wrong unit, and the leaf is the unit of *installation* rather than the unit of
*vendor*.

- The two containing directories exist only so that a machine that develops
  several integrations has somewhere to put them. They are never checked in,
  nothing depends on their names, and a contributor who clones one integration
  repo alone is in a perfectly normal situation.
- **This document lives in the core repository.** It governs all three families,
  but core is the one every integration already depends on, and its Design
  section is precisely the contract core publishes.
- **Every repository pins its Go toolchain** in `.tool-versions`. Integrations
  are built by people who are not us, on machines we do not control, and an
  unpinned toolchain makes "it does not compile" a conversation instead of a
  fact.

**[decided]** Dependencies point one way only: integrations depend on core, core
depends on no integration and contains no name of any integration. The only
tool- or agent-specific strings anywhere in core come from the user's
configuration file.

A consequence worth stating, because it is the price of this layout: **the
protocol and the record schema are interfaces between separately released
repositories.** They were a versioned change with a compatibility story until
2026-09-22, when D-77 observed that nothing here is published and every
repository is built and deployed together — so today a change to either is an
edit that lands everywhere at once, and the compatibility story comes back the
day agent-notify does get published.

### A3.2 The shape of the system

```
  claude ── fires hook ──▶ agent-notify-claude          (a Go binary; imports the core library)
                             │  translates: this agent's event  ──▶  what-happened + detail
                             │  captures:   allowlisted env vars + process ancestry
                             ▼
                          record-agent-event                          (core library, in the same process)
                             │  1. identify the session and the agent process
                             │  2. reduce (previous kernel state, what-happened) ──▶ kernel state
                             │  3. write the session record        ── the truth
                             │  4. ensure a session-watcher exists          ── detached spawn, never waited on
                             │  5. poke the session-watcher                 ── a hint, losable by design
                             ▼
                          session-watcher                        (one per user, long-lived)
                             ├─ watches each agent process for exit  (kqueue / pidfd)
                             ├─ sweeps the store for what died unobserved
                             ├─ derives container-coordinates from captured-context
                             ├─ coalesces changes and fans them out
                             └─ spawns and supervises the DISPLAYS
                                     │   (one unix stream connection each, for its whole life)
                      ┌──────────────┴───────────────┐
                      ▼                              ▼
          agent-notify-zellij-display        agent-notify-sketchybar
          pane and tab titles                counters and chips

                          the CONTAINERS                  (run when asked, never connected)
                             ├─ agent-notify-zellij-container     which pane, and go there
                             └─ agent-notify-aerospace-container  which window, and go there
```

### A3.3 Why the work is split exactly here

- **The hook process is the only place the context exists.** `$ZELLIJ_PANE_ID`,
  `$TMUX_PANE` and the chain of parent processes are visible to the agent's
  child and to nobody else, and they are gone the instant the hook exits. So the
  hook *captures*; it does not *interpret*. **[decided]**
- **The hook process is on the user's critical path.** Claude Code waits for its
  hooks. Everything that is not "capture, write, poke" belongs somewhere else,
  and the earlier design in which record-agent-event called every container
  synchronously is rejected for exactly this reason (§A19 D-4).
- **The session-watcher is the only place that can watch.** Death, elapsed time
  and fan-out all need a process that outlives any single event.
- **Displays are the only place presentation lives.** Core computes states. What
  a state looks like is a question only a display and the user's config can
  answer.

## A4. The life of one event

Numbered because each step has a rule attached to it.

1. **The agent fires a hook.** It execs the agent-integration binary with a JSON
   payload on stdin (Claude), or with arguments (another agent). Whatever the
   shape, this is the one part of the system that knows it.
2. **The agent-integration translates.** It decides two things and nothing else:
   *what happened* (a value from the closed event vocabulary) and *the detail*
   (a free string it alone chooses). It never decides what state the session is
   now in, because that answer depends on the previous state and it does not
   have one. **[decided]**
3. **record-agent-event captures context** — the allowlisted environment
   variables and the process ancestry chain — and resolves which ancestor is the
   agent process.
4. **record-agent-event takes the session's lock**, reads the previous record,
   reduces `(previous kernel state, what happened) → new kernel state`, merges
   the detail and the captured context, stamps time and sequence, and writes the
   record atomically.
5. **record-agent-event ensures a session-watcher exists**: it sends the poke,
   and if nothing is listening it spawns a detached session-watcher and returns
   immediately. It never waits for the session-watcher to come up, because the
   session-watcher reconciles from the store on startup and therefore loses
   nothing. **[decided]**
6. **record-agent-event exits 0**, having written nothing to stdout.
7. **The session-watcher wakes**, re-reads the changed record, and if this
   session is new to it, registers a process-exit watch on the agent pid and
   asks the containers to derive container-coordinates from the
   captured-context.
8. **The session-watcher coalesces and fans out.** Each connected display gets,
   at its own pace, the fact that this session changed.
9. **Each display renders** from the event, or from the store, or from both.

## A5. The state model: closed kernel, open detail

### A5.1 The rule

**[decided]** Every state is a pair.

```
kernel      one value from a small, closed, core-owned, versioned set
detail      a free-form, lowercase-kebab, agent-scoped string, or empty
```

```
working/compacting            blocked-on-you/permission-prompt
working/running-tool          finished-a-turn
broke/rate-limited            idle
```

- A display **groups, counts, orders and colours by the kernel**, so it works on
  the day a new agent it has never heard of appears.
- A display **may** additionally recognise a detail. If it does not, it degrades
  to the kernel and nothing breaks. This is the HTTP status-class property: a
  proxy that has never seen `418` still knows it is a 4xx and still does the
  right thing.
- **Core never interprets a detail.** It stores it, carries it, and hands it to
  displays verbatim.

### A5.2 Why the kernel can legitimately be closed

Because it is not a description of the agent. It is a description of **what the
state demands of the human**, and every agent that has ever existed is, at any
instant, either wanting something from you or not.

Defining it that way is what makes the mapping possible for agents we have never
seen: `compacting`, `running a tool`, `summarising`, `waiting on a subagent` are
all different inside the agent and identical to your attention, and the thing
that differs between them is exactly what the detail field carries.

### A5.3 What is deliberately rejected

**[decided]** Self-describing states — the agent-integration sending its own
glyph, colour and urgency rank so the display can render anything. Rejected for
three reasons: it hands your bar's theme to whoever wrote the codex adapter; it
makes "how many agents are waiting?" unanswerable, because every agent names
waiting differently; and it leaves the user unable to say "show compacting the
way you show working" without learning every agent's private vocabulary.

Dynamism belongs in the user's config, keyed on the pair:

```toml
[integration.zellij.settings.glyphs]
working = "*"
"working/compacting" = "~"
"blocked-on-you/permission-prompt" = "!"
```

### A5.4 Who reduces, and where

**[decided]** The reducer is one pure function in the core library, `(previous
kernel, what happened, elapsed) → kernel`, called from two places:

- **record-agent-event**, in the hook process, for transitions caused by the
  agent.
- **the session-watcher**, for transitions caused by observation — a process
  that died, and anything time-based we may add later.

This is what fixes the failure mode we found in both previous implementations:
an integration that must drop a real event (Claude's `SubagentStop`, which would
otherwise flash "awaiting" at you while the parent is still working) because it
cannot see the state it would be overwriting.

### A5.5 The urgency-rank: what lets a closed vocabulary grow

**[proposed]** A closed kernel is only safe if it never grows, and it will grow.
Displays live in repositories we do not control and are not rebuilt when core
ships a seventh state, so on that day every one of them meets a state it has no
glyph for.

The HTTP analogy in §A5.1 has a part we did not copy. `4xx` is a **class**; it
never grows, and it is what lets a proxy handle `418` correctly without ever
having heard of it. Our equivalent is a number.

- Every record and every delta carries the kernel's **urgency-rank** beside its name.
- A display meeting an unknown kernel **can** sort it correctly and paint it
  from its fallback for that urgency-rank, degrading to "something at this
  urgency" instead of to nothing. **This is a possibility we enable, not a
  promise we make**: core ships the urgency-rank and the SDK ships the fallback,
  and whether an unfamiliar state ends up looking right is between the display
  and its author. It survives D-77 because the unfamiliar state it protects
  against is a stored record's, not a newer binary's — a rank is what sorts a
  kernel that was renamed under a record that still names it.
- The urgency-rank also deletes the hardcoded order table every display would otherwise
  carry. zellij's tab glyph is `max(rank)` over its panes, an LED is `max(rank)`
  over everything, a picker sorts by it: one rule, computed identically
  everywhere, and the SDK provides it (R24).
- **Urgency-ranks are sparse**, assigned with gaps, so inserting a state later
  does not renumber its neighbours and a display keyed on urgency-rank keeps working.
- Urgency-rank is semantics, not presentation, so it is core-owned and not
  configurable. What an urgency-rank *looks like* is entirely the display's and
  the user's business.

### A5.6 The kernel states

**[decided 2026-09-17 — D-15]** Six, each named for what it demands of you,
ordered by the cost of your delay.

| Kernel | Urgency-rank | What it means |
| --- | --- | --- |
| `blocked-on-you` | 50 | it stopped mid-work and cannot continue until you answer |
| `broke` | 40 | the turn died; work stopped without finishing |
| `finished-a-turn` | 30 | it produced an answer and stopped; your move, at your leisure |
| `working` | 20 | it is doing something; nothing is required of you |
| `idle` | 10 | alive, nothing pending |
| `ended` | 0 | gone; the detail says why |

- `blocked-on-you` outranks `broke` because the cost of your delay is still
  running: a blocked agent sits there burning your wall-clock, a broken one has
  already stopped.
- **`broke` is a kernel rather than `finished-a-turn/failed`** because only
  kernels carry a count and an urgency-rank. A failed turn has to sort above a
  completed one — you deal with the breakage before reading the answer — and a
  detail cannot make an ordering claim. It also has to be countable on its own,
  since a failure hidden inside a pile of successes is the one you miss.
- A display is free to **collapse** kernels — a bar showing three lights out of
  six is a configuration decision, not a schema decision — and free to hide
  `ended` entirely.
- **`finished-a-turn` persists until you reply** (D-16). Nothing marks it as
  read: not elapsed time (D-12), and not focus. A read receipt was considered
  and rejected — driven by our own `focus` it would catch only the navigation
  that goes through us, and sampling what the window manager has focused would
  make the bar depend on a container being able to answer. So the state means
  "it produced an answer and is waiting for you", and it is true until you
  answer. The consequence is accepted deliberately: a bar left alone
  accumulates, because unanswered agents really are accumulating.
- `ended` is a kernel rather than a separate lifecycle flag so that a display
  checks one field instead of two. Why it ended is a detail: `ended/exited`,
  `ended/process-gone`, `ended/superseded`.

### A5.7 The events, and the reducer

**[proposed]** Nine events, closed and versioned. An agent-integration maps its
agent's hooks onto these, and **may map an event to nothing**: Claude's idle
nudges, auth-success notices and quota chatter must never escalate, and knowing
which is which is agent-specific knowledge that belongs in the adapter.

```
session-started    user-sent-prompt    agent-progressed    turn-finished
turn-failed        turn-interrupted    blocked-on-human    session-ended
context-changed
```

The ninth arrived with codex in M11 and is the only one the design gained while
being built (D-36). It is listed here rather than in a footnote because the
reason it was needed is the argument for the list being closed at all: one
agent's hook could not be translated honestly into the other eight, which is
exactly the signal that the vocabulary was short by one — and not the signal
that the agent should be allowed to invent its own.

The reducer, `(previous kernel, event) -> kernel`:

| Event | Becomes | Note |
| --- | --- | --- |
| `session-started` | `idle` | **only if the session was not already live** — see below; the condition is not a concession to any agent |
| `user-sent-prompt` | `working` | |
| `agent-progressed` | `working` | a finished subagent maps here — the event both previous implementations had to throw away, because an adapter could not see the parent state it would overwrite. **[verified 2026-09-17 — D-32]** Claude's `SubagentStop` fires for its own turn wrapper as well, and *that* one must be discarded by the adapter; see M7 |
| `turn-finished` | `finished-a-turn` | |
| `turn-failed` | `broke` | |
| `turn-interrupted` | `idle` | the turn was stopped on purpose by the person sitting there: nothing was produced, nothing is owed, and nobody needs telling (D-36) |
| `blocked-on-human` | `blocked-on-you` | |
| `session-ended` | `ended` | |
| `context-changed` | unchanged | metadata only: cwd, model, name |

**[verified 2026-09-17, M3]** The table's three "unchanged" cases are one rule,
not three. `session-started`'s condition, `context-changed`'s metadata-only
behaviour, and what to do with an event a newer adapter invented all reduce to:
*leave a live session exactly where it is, and bring a dead or absent one back
as idle.* The reducer has one default branch, and the resurrection rule below
falls out of it rather than being coded separately.

**[decided 2026-09-17 — D-31] Why `session-started` is conditional, and why that
is not core absorbing an agent's mistake.** An earlier version of this table
justified the condition with "Claude fires this on compaction and on resume
too", which put an agent's hook behaviour into core's reasoning and was wrong on
both counts.

- **An adapter that emits `session-started` for a compaction is
  mistranslating.** Claude's `SessionStart` carries a `source` —
  `startup`, `resume`, `compact` — and a compaction is the agent working, not a
  session beginning. It maps to `agent-progressed` with detail `compacting`,
  which is the honest translation and also the more useful one: it is what puts
  `working/compacting` on the bar. M7 says so in the adapter, which is where
  agent semantics belong.
- **The condition stands on its own, agent-independently**, because
  `session-started` asserts that a session *exists* rather than that something
  just changed, and asserting the existence of something that already exists is
  idempotent by nature. There is no agent for which resetting a live session to
  `idle` is the right answer: it destroys information and gains none.
- **And it costs nothing, because it is not a special case.** The default branch
  exists anyway, for `context-changed` and for events a newer adapter invented.
  `session-started` merely falls into it. Making it unconditional would mean
  *adding* a case whose only effect is to lose state.

Three rules the table does not show:

- **Any event arriving on an ended session resurrects it.** Resume therefore
  works even for an agent with no session-start hook at all, and it is the
  general rule of which `session-started` is only the common case.
- **`sequence` continues across a resurrection.** It never restarts, so no
  display ever sees it go backwards.
- **Transitions caused by observation are the session-watcher's** — death and
  supersession, and those only. **Elapsed time never changes a state** (D-12).
  Same reducer, different caller (D-8).

### A5.8 Details

**[proposed]** One **shared, unprefixed** namespace, lowercase-kebab.

- `permission-prompt` means the same thing whoever wrote it. Prefixing details
  per agent would force a user to write one glyph rule per agent for a concept
  they think of as one thing.
- Conventions are documented and encouraged, never enforced: enforcing them
  would mean core holding a list of each agent's private vocabulary (R10).
- Core never interprets a detail (R7). It stores it, carries it, and hands it to
  displays verbatim.

### A5.9 Nothing is open here

The state model is settled: the kernel-and-detail split (D-5), the urgency-rank
as an enabler rather than a promise (D-14, amended by D-77), the six states
(D-15), no read
receipt (D-16), and one message field (D-17). What remains marked proposed in
§A5.7 and §A5.8 — the event list, the reducer table, the detail conventions — is
detail to be confirmed against real agent hooks while building, not open design.

**[amended 2026-09-18 — D-36]** That confirmation happened, and it cost one
event. The six kernels were not touched, the reducer's shape was not touched,
and no agent's vocabulary reached core; what changed is that the event list is
now nine rather than eight. That is the process working: "confirmed against real
agent hooks while building" means the list is allowed to be wrong in a way the
second agent can prove, and being short by one is the cheapest way for it to
have been wrong.

## A6. Session identity

**[decided]** A session is keyed by the triple:

```
(host, agent, session-id)
```

- `session-id` alone is not unique: two agents can mint the same string, and
  they mint them under different schemes.
- `host` is in the key from the first commit even though v1 is local-only,
  because adding it later changes every stored filename and every display's
  notion of identity.
- The key is rendered into a filename by a reversible, filesystem-safe encoding;
  agent ids are not guaranteed to be path-safe.

Two facts about identity that are not obvious and that the store must survive:

- **A resumed session keeps its id.** Claude hands back the same session id on
  `--resume`; `--fork-session` exists precisely to opt out. So an id reappearing
  after the session ended is *the same session coming back*, not a new one.
- **One process can outlive its session.** `/clear` starts a fresh session
  inside the same running agent process, so two records can name one living pid
  and only the newest is real. The older one has ended even though its process
  is alive.

## A7. The session store

### A7.1 The store is the truth

**[decided]** Everything that crosses a socket is a *hint*: "session X changed,
sequence 47". The content lives in the store. A display that missed a message,
started late, or crashed and came back re-reads and is correct.

This single decision buys three things: sockets need no delivery guarantee,
overflow can be handled by dropping, and a session-watcher restart costs
nothing.

### A7.2 Where it lives

**[decided]** Durable state and volatile runtime state are separated, because
one must survive a reboot and the other must not.

```
state directory     Linux   ${XDG_STATE_HOME:-~/.local/state}/agent-notify
                    macOS   ~/Library/Application Support/agent-notify
    sessions/<key>.json         live records
    ended/<key>.json            filed away, still resumable
    history/<key>.json          the last N messages and N state changes (§A7.7)
    locks/<key>.lock            never renamed, never deleted while referenced

runtime directory   Linux   ${XDG_RUNTIME_DIR}/agent-notify
                    macOS   ${TMPDIR}/agent-notify            (already per-user, mode 0700)
    session-watcher.lock        the singleton mutex
    subscribers.sock            stream: session-watcher <-> integrations
    session-changes.sock        datagram: record-agent-event -> session-watcher
    agent-notify.log            written by both processes, named after neither

configuration       ~/.config/agent-notify/config.toml  (XDG_CONFIG_HOME honoured on both)

AGENT_NOTIFY_ROOT   <root>/state, <root>/run, <root>/config.toml
```

- Both directories are created mode `0700` and every file `0600`. Records carry
  the text of what an agent last said; that is not world-readable material.
- One environment variable overrides all three. **[decided 2026-09-17 — D-20,
  extended by D-24]** `AGENT_NOTIFY_ROOT` takes the configuration file with it as
  well as the two directories, because an isolated instance that still reads your
  real configuration is not isolated.
- **[verified 2026-09-17, macOS 26]** `sun_path` is a 104-byte array and the
  kernel needs the terminating NUL, so **103 bytes is the longest path that
  binds**; 104 fails with `EINVAL`, for `unix` and `unixgram` alike. The check
  is against 103, not 104 — checking against the struct size would admit exactly
  the one path that then fails at `bind`, which is the failure the check exists
  to prevent. Linux's 108-byte array gives 107 the same way (**[assumed]**, not
  yet measured).
- **[verified 2026-09-17]** `t.TempDir()` on macOS returns a path under
  `$TMPDIR` long enough that appending `run/session-changes.sock` exceeds 103.
  Every test from M8 onwards that binds a real socket must use a short root under
  `/tmp`, not `t.TempDir()`. The default runtime directory is unaffected, at 82
  bytes for the longest socket.
- **[decided]** When `XDG_RUNTIME_DIR` is unset on Linux, and when `TMPDIR` is
  unset on macOS, the runtime directory falls back to
  `$TMPDIR/agent-notify-<uid>`. The uid suffix is not decoration: `/tmp` is
  writable by everyone, so an unsuffixed name there is a name another user can
  take first.

### A7.3 How a record is written

**[decided]** record-agent-event is a short-lived process and several can run at
once, for the same session (two hooks firing together) or for different ones.

1. Open `locks/<key>.lock` and take `flock(LOCK_EX)` with a short deadline. The
   lock file is never renamed, so the lock always refers to the same inode — the
   classic bug of locking a file you are about to replace by rename.
2. Read the current record.
3. Reduce, merge, stamp.
4. Write a temporary file in the same directory, `fsync` it, `rename` over the
   target, `fsync` the directory. A reader therefore never sees a half-written
   record — it sees the old one or the new one, because a record is replaced
   and never edited in place.
5. Release the lock by exiting; `flock` is released by the kernel even on
   `SIGKILL`.

**[verified 2026-09-17, macOS 26 — D-28] `fsync` means `fsync`, and Go's
`(*os.File).Sync` is not it.** On darwin that method issues `F_FULLFSYNC`, which
waits for the drive to flush its own cache: **5.8ms per write against 160µs**,
measured on a 1KB record written by temporary file and rename. An event can
write two files, so it would have put around 12ms on every hook — on the one
path that must never make the agent wait (R1). The store calls `syscall.Fsync`.

What that gives up is durability across a power cut, and it costs nothing here:
a reboot changes the boot id, which ends every session in the store anyway
(§A8), so a record that did not reach the platter was describing a process that
no longer exists.

**[decided 2026-09-17 — D-28] A record moving between `sessions/` and `ended/`
is written before it is removed**, never the other way round, because two files
can be resolved and zero cannot. When both exist — a process died between the
two steps — **the higher `sequence` wins**, which is already the rule for
ordering anything in this system (R16), and the next write removes the loser.
No repair pass and no timestamps are needed.

**[decided 2026-09-17 — D-28] A writer that cannot get the lock gives up.**
`sessionstore.LockPatience` bounds the wait; on expiry record-agent-event logs
and exits 0.
Losing one event is cheap, because the next one re-establishes the state and a
subscriber is level-triggered anyway (R22). Hanging the agent is not cheap, and
R1 does not bend.

Stamped by core on every write, never by a caller:

- `sequence` — monotonic per session, incremented on every write. A display that
  sees a gap knows it missed something and re-reads. Ordering that matters is
  expressed here, never inferred from the transport.
- `updated_at` — UTC, ISO-8601, string. Stored as text so a foreign reader needs
  nothing but a JSON parser, and in UTC so lexicographic order is chronological.
- `state_since` — moves **only when the kernel state changes**, so "how long has
  this been waiting?" survives every unrelated write. This is the field a bar
  needs to sort by urgency.

### A7.4 The shape of a record

**[decided]** One structural rule, a consequence of §A3.1: **core must be able
to store what it cannot understand.** Anything written by an integration or by
an agent lives in a section named after its owner, and core carries it without
inspecting it. Adding an integration must never mean editing core.

**[proposed]** The fields, each one traceable to a display in §A12.1 that
demanded it. Nothing is here because it seemed useful.

| Field | Written by | Why it exists |
| --- | --- | --- |
| `key` = host + agent + session-id | core | identity (§A6) |
| `sequence` | core | gap detection and ordering (R16) |
| `kernel`, `rank`, `detail` | core | every display |
| `state_since` | core | "waiting 12m", sorting, future escalation |
| `created_at`, `updated_at` | core | age, staleness |
| `name` | out of band, §A7.4.2 | zellij pane titles, chips, the picker |
| `cwd` | the agent-integration | the picker, and the name fallback |
| `message` | the agent-integration | chip hover, picker preview. **One field, not several** (D-17) |
| `process` = pid + start time + boot id | core | liveness (§A8); no display reads it |
| `ended_at` | core | retention counts from here; cleared by resurrection |
| `captured_context` = allowlisted env + ancestry | core, in the hook | containers derive container-coordinates from it (§A11) |
| `annotations.<owner>` | anyone | container-coordinates, git branch, task ids (§A7.4.1) |
| `usage` | the agent-integration | tokens spent, in one vocabulary both agents fill (§A7.4.3) |
| `branch` | core, from the checkout at `cwd` | which branch this session is on (§A7.4.4) |
| `model` | the agent-integration | what a display groups and prices by (§A7.4.4) |

**[decided 2026-09-17 — D-17] `message` is one field, overwritten by whatever
event last carried text.** The `(kernel, detail)` pair already says how to read
it: `blocked-on-you/permission-prompt` means it is an ask, `finished-a-turn`
means it is an answer, `broke` means it is an error. Separate fields —
`last_said`, `pending_ask`, `last_error` — were rejected because the per-session
history (§A7.7) already serves the "show me the previous one too" case, and
because deciding which agent event fills which slot would put agent semantics in
the schema, which is the job the detail label already does.

The cost, stated so it is not a surprise: a `working` session's `message` is
whatever it last said or last did, so the text can be older than the state. A
display that cares shows `state_since` beside it.

**Cut deliberately**, so that we do not re-argue them: `message_at`; a separate
`last_said` kept alongside `message`; model; token counts; cost; transcript
path. **[decided 2026-09-17 — D-25, while building M3]** And `ended_reason`,
which this table listed and §A5.6 had already contradicted: why a session ended
is `ended/exited`, `ended/process-gone`, `ended/superseded` — a detail, like
every other refinement of a kernel. Two fields for one fact is two fields that
will disagree. The rest were not so much cut as **pushed into `agent_data`**,
which is what an owned namespace is for — a display that wanted the model read
it there, and core never learned the word "model".

**[amended 2026-09-20 — D-61]** Token counts came back out. `agent_data` is the
one place they could not be read from — it is opaque by construction (R7), so a
display reading tokens out of it needs one code path per agent, which is promise
1 spent on a namespace. They are a field, and §A7.4.3 is what they are. `cost`
stays cut, and stays cut for the reason it always was.

**[amended 2026-09-22 — D-76]** `agent_data` itself is gone, and with it the
namespace this paragraph offered as the home for everything cut. What it was
actually carrying — a transcript path, a permission mode, a prompt id, an effort
level — was read by nothing in three months. So the list above is now simply
cut, with no second home, and an integration that turns out to need one of those
facts asks for it as a field and argues for it here like everything else.

**`captured_context` is kept, not dropped once container-coordinates are
derived**, because interpretation happens later and elsewhere: an integration
that was not running when the hook fired interprets the stored blob when it
connects (R4). It is also what makes interpretation testable without the tool —
a stored blob replayed through `interpret-environment` with no zellij in sight.

#### A7.4.1 Three sections, three lifetimes

**[decided 2026-09-17 — D-27, resolving Q15]** A git integration wants to attach
the branch. An orchestrator wants to tag a session as "task 3 of 7". A container
wants to record which pane it is in. None of them is the agent, so none belonged
in `agent_data` — the agent's own section, which is itself gone since D-76 and
which none of the three would have gone into anyway.

This section used to put all three in one `annotations` map, on the grounds that
one write path and one set of rules should serve everybody. That is what raised
Q15, and the answer turned out to be that **the map was wrong, not that it
needed a provenance tag per entry**. Three things with three lifetimes get three
homes:

| Section | Who writes it | How long it is true |
| --- | --- | --- |
| `captured_context.by.<owner>` | core, in the hook, on behalf of each integration | until the agent's process changes, or that integration captures again (D-90) |
| `derived_context.<owner>` | an integration, interpreting a captured context | exactly as long as the capture it came from |
| `annotations.<owner>` | anyone, unsolicited — an integration pushing, or a human at the CLI | until its owner overwrites it |

- **The lifetime rule is one line and needs no bookkeeping**: the writer that
  replaces a captured context clears the derived context in the same write.
  **[amended 2026-10-09 — D-90]** Per integration: replacing `by.zellij`
  clears `derived_context.zellij` and nothing else, and a new process replaces
  the whole capture and clears the whole derived context.
  Because that is a single atomic record write (§A7.3), no reader ever compares
  timestamps, and there is no window in which a stale coordinate looks fresh.
- **Nothing is tagged, because nothing needs to be.** Which section a write
  lands in is decided by how it arrived: a reply to the session-watcher's
  invitation is derived by definition, and an unsolicited push is not. The
  provenance *is* the protocol.
- **Why this is correctness and not tidiness.** A pane id worked out from an
  environment that no longer applies does not merely go out of date. Pane 7 now
  belongs to somebody else, and a display that focuses it takes you confidently
  to the wrong place, which §A11.3 calls worse than not going at all. Meanwhile
  "task 3 of 7" is still true after you resume in a different pane, and deleting
  it would destroy something nobody can rebuild — the CLI writer is a one-shot
  process that exited days ago, while an enricher is connected and re-answers in
  milliseconds.
- Core inspects none of the three (R7).
- Written through one entry point (§A16), and by the enricher capability
  (§A10.3) which the session-watcher invites when a session is created or
  resumed. **[decided 2026-09-17 — D-11] Ownership is derived, never policed.**
  There is no boundary to enforce: every writer runs as the same user, the
  directories are `0700`, and the configuration is already trusted (§A15). A
  permission check here would be theatre — anyone who can call `annotate` can
  edit the JSON directly.

The risk that is real is accident, and one accident in particular: an
integration writing under `zellij` when it is not zellij corrupts a session's
container-coordinates, and corrupted coordinates are worse than missing ones,
because focus then takes you somewhere confidently and it is the wrong place
(§A11.3). So:

- **A connected integration never names an owner.** It named itself in the
  handshake, the session-watcher stamps that name, and the field simply does not
  exist in its message. It cannot lie because it cannot speak.
- **The CLI may write anything**, under any owner. That is correct: a human
  running a command is the user, and the user may do anything.
- **Lifecycle follows the section, and the section follows provenance.** This
  is why container-coordinates vanish on resume (§A8.6), and why a git branch
  vanishes with them and is re-derived for nothing — which costs nothing,
  because the enricher that derives it is connected and answers at once.
- **An owner's section is replaced wholesale, never merged.** Merging would mean
  core reasoning about a shape it is forbidden to understand (R7), and an owner
  always knows its own full state.
- **Bounded per owner and in total.** Annotations ride in every delta, and one
  integration storing a megabyte must not degrade everyone else (R13).
- Writing one bumps `sequence` and wakes whoever cares, but **never moves
  `state_since`** — the same rule a reported name keeps (§A7.4.2).

#### A7.4.2 Where the name comes from

**[decided 2026-09-19 — D-60]** The name comes from **the agent**, reported by
its agent-integration on the hooks it was firing anyway. `Report.Name` is an
optional field beside `Cwd`, and it behaves like one: present means set, absent
means leave what is stored alone, and no event ever clears it.

This reverses the previous answer, which was that a session names itself by
running `agent-notify name "<name>" --if-unnamed` as a habit written into the
agent's instructions. That command is gone. What was wrong with it:

- **It asked an agent to do by instruction what its agent already does by
  itself.** Both agents keep a name. Claude generates one with the prompt
  *"Generate a short kebab-case name (2-4 words) that captures the main topic of
  this conversation"* — word for word the habit it was being taught — and codex
  names every thread. A tool call per session to re-derive what is already on
  disk is a tool call to delete.
- **It cost a write nobody needed.** A name riding on an event that was being
  written anyway costs nothing; a command costs a process, a lock and a
  sequence of its own.
- **It only ever fired once.** A rename halfway through a session never reached
  the record. Carrying the name on every hook means the next tool call fixes it.

What survives intact is the rule the old design existed to protect: **a name is
a label and never a state.** It rides on whatever event carried it and does not
move `state_since` by itself — "waiting 12m" has to survive being labelled —
and an event that carries no name leaves the stored one alone, which is what
`--if-unnamed` used to be for.

Where each agent keeps its name is the agent-integration's business and core
never learns it (R10). Both are a file read on the hook path — no process, no
socket, and nothing that can fail louder than an empty string:

- **Claude** keeps `~/.claude/sessions/<pid>.json` per live session, rewritten
  as it runs. The hook payload does not carry the name — [verified 2026-09-19,
  2.1.236] every hook input is built from the same six fields and only the
  statusLine command is given `session_name` — so the file is read instead,
  found by `CLAUDE_PID` and confirmed by matching `sessionId`, because pids are
  reused and `/clear` replaces the session inside a process without renaming the
  file. That file is not the only place a name can come from, and on its own it
  is usually the worst one: two of the four values of `nameSource` are the
  placeholder `<cwd-basename>-<counter>`, which is what a session is called when
  nobody has named it. The head of the transcript is read as well, for the
  `ai-title` Claude generates, and the best of three is reported — a `/rename`,
  that title, Claude's own label — with **nothing at all** reported when Claude
  has none of them (D-75).

  **[amended 2026-10-09 — D-88]** The payload does carry the name now, on two
  hooks: `UserPromptSubmit` and `SessionStart` send `session_title` when the
  session was given one, and it is read before the file. `nameSource` has six
  values rather than four, `hook` and a missing one are names somebody chose,
  and the placeholder ends in a random byte, not a counter.
- **Codex** keeps the name in `threads.name` in `~/.codex/state_5.sqlite` and
  projects it into `~/.codex/session_index.jsonl`, one line per named thread,
  newest last. The projection is read rather than the database: a hook has no
  business opening somebody else's sqlite in WAL mode while they write to it.
  It is read backwards from the end, 64 KiB at a time, stopping at the first
  read that holds the id: entries are in the order threads were named, so the
  first read almost always ends it, and a session left open while six hundred
  others are named is found by the read before rather than lost. [verified
  2026-09-19, 0.154.0] against a real TUI session —
  the name is generated about four seconds into the first turn and the line is
  written then, so every hook after it carries the name. A headless `codex
  exec` thread is never named at all and falls back to the cwd.

- **The fallback belongs in the SDK**, not in the record and not in each display
  (R24). The record stays honest, an empty name means unnamed — reliably, since
  D-75 — and every display gets identical behaviour for nothing. It is the last
  component of `cwd` plus two characters of the session id, `agent-notify-8e`,
  because the three sessions you have open in one repository would otherwise all
  be called the same thing; it is per-record and stable, because `DisplayName`
  is an address as well as a label and `agent-notify focus <name>` matches on
  it. `Record.Named()` is how a display asks which of the two it is holding.

#### A7.4.3 What a session has spent

**[decided 2026-09-20 — D-61]** Tokens are a field of the record, reported by
the agent-integration on the hooks it was firing anyway, in one vocabulary both
agents fill. Every agent that is worth watching keeps this accounting already
and keeps it somewhere a hook can read; what it does not keep is a shape any
display could read without knowing which agent wrote it, and that shape is the
only thing core adds.

| Field | What it is |
| --- | --- |
| `usage.input`, `usage.output` | fresh tokens in and tokens out, counted over the whole session |
| `usage.cache_read`, `usage.cache_write` | the same two, for the halves of prompt caching |
| `usage.reasoning` | **of which** `output` was thinking. Not a fifth addend |
| `usage.counted_through` | the last response counted. Opaque to core, compared only for equality |

- **The first four are disjoint and the sum of them is the total**, which is the
  property that lets a display price them: a cache read costs a tenth of a fresh
  input token and a cache write costs more than one, so a single blended number
  is not a number anybody can use. `reasoning` is the one figure that is a
  subset rather than an addend, and it is named for it.
- **No total**, because every subscriber adds up the same four and gets the same
  answer, which is R26 — the rule that keeps `Elapsed` computed and out of the
  record.
- **No cost in currency, and that is not a gap.** Pricing is a per-model rate
  table that changes underneath you, and §A14 already refuses a fact about a
  program in the configuration. Core counts tokens; what they are worth is the
  display's business, with the record's own `model` beside them.

**[amended 2026-09-22 — D-76]** There were two more rows: `usage.context`, what
was in the window on the last request, and `usage.context_limit`, what that
window holds. Both are gone.

- **Only one of the two agents could ever fill the pair.** codex hands over
  `model_context_window`; Claude's transcript has no equivalent — [verified
  2026-09-20, 2.1.236] a session running the 1M window records
  `"model":"claude-opus-5"`, the same string the 200k one records, so the file
  cannot be made to say which it is.
- **Which made the one display that drew it draw it for one agent.** A limit
  nobody reported meant `ContextFilled` answered "cannot tell" for every Claude
  record ever written on this machine [verified 2026-09-22, against the four in
  the store], so the meter in the picker only ever appeared beside codex.
- **And the level was being stored on every record regardless**, moving on every
  response, for a percentage that was drawn for one agent out of two. That is
  the trade §A7.4 exists to refuse.

##### The adapter reports responses; core does the adding

An adapter cannot report a running total, because it is a one-shot process that
exits in milliseconds and the running total is in the record it has not written
yet. It could re-read its agent's whole transcript on every hook — [verified
2026-09-20] one of them here is 51 MB and 3401 assistant lines — and that is a
cost that grows with the session for as long as the session lasts.

So a report carries **the responses one read of the tail of the agent's file
could see**, oldest first, each with the agent's own id for it, and `Apply` adds
the ones after `counted_through`. The agent-specific half stays in the adapter,
where the file format is; the arithmetic stays in core, where the previous total
is.

- **The id is what makes it correct, not merely idempotent.** [verified
  2026-09-20, 2.1.236] Claude writes one transcript line per content block and
  repeats the response's entire `usage` on every one of them — 3401 assistant
  lines carrying 1869 distinct `requestId`s — so summing lines overcounts output
  tokens by 2.11× and cache reads by 1.77×. Deduplicating is not a refinement
  here; it is the difference between a number and a wrong number. codex's
  `token_usage_record` carries `response_id` and has the same shape, and core
  reads neither: it compares strings.
- **A window that no longer reaches the cursor adds everything it holds**, which
  undercounts by whatever fell off the end rather than counting twice what did
  not. [verified 2026-09-20] the last 64 KiB of that 51 MB transcript holds 14
  responses, against a hook that fires on every tool call, so the window is a
  margin and not a bound.
- **Two hooks racing cannot double-count**, and that is a property of agents
  rather than of luck: a model response cannot arrive until every tool result
  from the one before it has, and every one of those is a hook that has already
  run.

##### It wakes nobody

A counter that moves on every response would turn a fifty-tool-call turn into
fifty renders on every display, which is the exact thing `Differs` exists to
stop (R23, §A9.3). So `usage` is subtracted before the default question is
asked: tokens ride along in the delta that some other change produces, and a
display that wants every tick names `usage` in its `wake_on`.

**That makes two kinds of field the comparison has to keep apart, and the first
implementation did not.** A stamp — `sequence`, `updated_at` — is worth nothing
to anybody, ever. An opt-in field is worth everything to whoever asked for it
and nothing to everybody else. The session-watcher asks **one** question before
it offers a record around at all, and it was asking the default one, so a write
that moved only `usage` was dropped before any subscriber was consulted and the
display that had named `usage` never heard about the field it named. [verified
2026-09-20, live] the store went from sequence 143 to 147 with the totals
climbing, and a subscriber asking for `usage` received nothing but its opening
snapshot.

The gate and the wake are two different questions, and the fix is to let them be
two: `WorthOfferingAround` ignores the stamps and nothing else, and `Differs`
with no fields named ignores the opt-in fields as well. A gate that answers the
narrow question on a subscriber's behalf decides, for that subscriber, that it
did not mean what it said.

The write costs nothing either, and for the same reason the name costs nothing
(§A7.4.2): the record is being written on that hook anyway. What changes is that
the record is about a hundred bytes longer.

**What this does not buy.** Claude's number excludes its own subagents.
[verified 2026-09-20] not one transcript on this machine holds a single
`isSidechain` assistant line, and `SubagentStop` carries a separate
`agent_transcript_path` — so a Task-heavy session under-reports until that
pointer is followed too. That is a second cursor per subagent transcript, and it
is deliberately not in this.

#### A7.4.4 What a session knows about itself

**[decided 2026-09-20 — D-65]** Three more fields, on the same terms as the
tokens: reported on hooks that were firing anyway, best-effort, and empty when
the answer is not there. They were `model`, `branch` and `quota`, and each one
answered a question a display could not ask before. **[amended 2026-09-22 —
D-76]** `quota` is gone; the two that are left are below, and why the third went
is at the end of this section.

| Field | Where it comes from | Why it is a field of its own |
| --- | --- | --- |
| `model` | the agent, on every hook or every response | a bar counts models across agents, and an opaque namespace can only be read by somebody who already knows which agent wrote it |
| `branch` | core, from the checkout at `cwd` | it is not the agent's fact at all |

- **`model` was already listed in `agent_data` and was not actually there.**
  [verified 2026-09-20, 2.1.236] Claude's hook payload has no `model` field —
  every hook input is built from the same six fields — so the claude adapter had
  been writing an absent key since the day it was added, and every record on
  this machine carried `agent_data` with no model in it. Codex's payload does
  carry one. So the two adapters answer differently: codex reads its payload,
  Claude reads `message.model` off the newest assistant line of the transcript
  it is already tailing for the tokens. That is what an adapter is for, and it
  is free on both sides.
- **The newest response is the one that counts**, because `/model` mid-session
  is ordinary and every older line in the file still names what used to be true.

##### Quota was about the account, and that was not why it went

`quota` was a list of allowances — a name, a fraction used, how long the window
is, when it starts again — describing the **account** rather than the session.
Every session of that agent therefore carried the same answer and the most
recent write was the truest. That much was right, and the objections raised
against it at the time were all answered:

- **It was stored per-session because a record is the only thing a display is
  ever handed.** A second store keyed by agent would have been a second
  lifetime, a second retention rule and a second thing to keep consistent, for
  one number.
- **`null` is not zero.** Codex writes `"secondary": null` for an allowance that
  does not apply, and a decode that turned that into a zeroed window would have
  painted a limit that does not exist. The adapter dropped it.

**[decided 2026-09-22 — D-76]** It went anyway, and for the reason that had
nothing to do with any of that: **only codex ever answered.** Claude's
equivalent lives in the statusLine payload, which is the user's slot (§A7.4.2)
and not ours to take, so a Claude session carried no quota at all — and Claude
is what this machine actually runs. A field that one agent of two can fill is a
field every display has to write a "when it is there" branch for, and the one
display that wrote that branch drew a meter that had never once appeared.

What it cost to keep was not nothing: the codex adapter read a second kind of
rollout line for it, decoded two nullable allowances and converted both, and
`quota` rode on the record outside the `usage` opt-in — so a quota that moved on
every `token_count` woke the gate that asks whether anybody could want a change.

This is a decision about maturity and not about design. When both agents report
an allowance in a place an adapter may read, `quota` comes back as it was
written here, and this section is the argument for it.

##### Branch is read by core, and R10 bends

**R10 says core contains no name of any tool.** `hook/internal/branch` contains one,
deliberately, and the rule is amended rather than quietly broken.

- **Neither agent can be trusted with it.** [verified 2026-09-20, 2.1.236]
  Claude puts `gitBranch` on every transcript line, and on this machine it says
  `"HEAD"` for a repository sitting on `main` — because the session's directory
  is a workspace that merely *contains* checkouts, and Claude answered anyway.
  Codex writes `git: {branch, commit_hash}` once, in `session_meta`, at the head
  of the rollout: the branch as of session start, which a session that checked
  out something else reports wrongly for the rest of its life. A confidently
  wrong branch is the hazard §A11.3 is about.
- **The rule's purpose is served by breaking its letter.** R10 exists so that
  per-tool knowledge lives in the integration that owns the tool. There is no
  such integration here and there is not going to be: every agent runs in
  somebody's checkout, so an adapter-side answer is the same forty lines written
  once per agent forever, which is what R24 exists to stop.
- **It reads files and never runs git.** `.git/HEAD` is either `ref:
  refs/heads/<name>` or a commit; the first is the answer and the second is a
  detached head, which has no branch and reports nothing. A `.git` that is a
  *file* is a worktree, and following its `gitdir:` is not an edge case to
  tolerate but the case that has to work — a worktree is how two agents run on
  two branches of one repository at once, which is most of the reason to want
  this on a bar.
- **Empty is an ordinary answer** and there are three ways to reach it: no
  repository above `cwd`, a detached head, and anything unreadable. The first is
  not rare — a session opened on a directory that holds three checkouts has no
  one branch, and saying so is better than picking one.
- It costs a few `os.Stat` calls on a path that already tolerates a second of
  capture (§A10.3), and it is recomputed every hook, so a mid-session checkout
  lands on the next tool call.

**None of the three moves `state_since`**, and none of them is a stamp: a model,
a branch and an allowance change rarely and visibly, so unlike `usage` they wake
a display that named no fields at all.

### A7.5 Retention

**[decided 2026-09-17 — D-26]** There is **one** retention duration.

```toml
keep-ended-sessions = "168h"   # seven days (D-78)
```

It is how long an ended session's record survives, so that resuming it is
recognised as a return rather than a birth — its sequence continuing, its age
and its name intact. It must comfortably exceed how long you leave a session
closed before you come back to it.

**A second duration was here and is now gone.** `store-retention: 1000` from the
first sketch was doing two unrelated jobs, and splitting it into "how long it
stays known" and "how long it stays visible to displays" looked like the fix. It
was not, for three separate reasons, any one of which is sufficient:

- **It is presentation policy in core**, which R9 puts in the user's
  configuration and R8 puts behind the kernel. Whether an ended session is worth
  a row on your bar is exactly the kind of question core has no business
  answering.
- **It makes a display's contents change because the clock moved.** At the
  ten-minute mark a connected subscriber must be told something it was not told
  at the nine-minute mark, and nothing happened in between. R26 already names
  this: a fact every subscriber can derive identically from the record it
  already holds is rendering, not a state change. `ended_at` is in the record.
- **It breaks the picker.** A picker's whole job is "show me the sessions I can
  still resume", which is the set bounded by `keep-ended-sessions` — days. A
  core that stops delivering ended sessions after ten minutes makes that
  impossible to answer from the socket, and pushes the picker back onto a cold
  file walk to do the one thing it exists for.

So visibility is not a duration at all. It decomposes into two things that were
always going to exist anyway:

- **A subscriber says whether it wants ended sessions in its opening snapshot**,
  alongside what else it declares (R23). A bar says no. A picker says yes.
- **The transition to `ended` is always delivered**, to everyone, regardless.
  It has to be: it is how a bar learns to remove the row, and how a notifier
  learns your agent died. "Do not include ended sessions in the snapshot" and
  "do not tell me a session ended" are different requests and only the first is
  available.

A display that genuinely wants a finished session to linger for two minutes
computes that from `ended_at` and renders it, configured in its own `settings`
where its glyphs already live (D-24). Core is not involved.

**[decided]** There is no second duration for history either. Session history
(§A7.7) is bounded by **count, not by time**, and dies with the session it
belongs to, so it needs no retention rule of its own and it cannot outlive what
it describes.

### A7.6 The read path

**[proposed — resolves Q3]** The open question was whether displays walk
`sessions/` themselves or read a snapshot file the session-watcher maintains. It
dissolves: the session-watcher already holds every record in memory, so it
serves the connect-time snapshot **from memory** over the socket (§A12.2). There
is no `snapshot.json`, and no second thing to keep correct.

What remains of the file read path, and why it must stay fast:

- **Cold reads with no session-watcher.** `agent-notify list` must work when
  nothing else is running. It walks `sessions/` and is done.
- **[decided 2026-09-17 — D-30] `list` applies the liveness decision as it
  reads, and writes nothing.** A session whose process was killed must not be
  shown as working merely because nothing has got round to filing it — but
  filing it is a transition, and transitions belong to the session-watcher
  (§A8.6). A command a statusline polls three times a second has no business
  writing to the store.
- **A statusline inside an agent.** Claude Code's statusline, polled every few
  hundred milliseconds to show your *other* agents, is the strongest reason this
  path must never require a socket: N small file reads at tens of sessions, no
  session-watcher dependency, no handshake.
- A display reads files only on a cold start with no session-watcher present.
  Its steady state is the opening snapshot plus deltas.

### A7.7 History: what a session carries, and what we refuse to keep

**[decided 2026-09-17 — D-10]** Two different things were being called history,
and collapsing them was an error:

| | Kept? |
| --- | --- |
| **session history** — the last N messages and the last N state changes of a session that still exists; bounded by count; deleted with it | **yes** |
| **a global event log** — every transition of every session, retained by time, outliving the sessions themselves | **no** |

#### Why the global log is refused

The case for it rested on a notifier that must not miss a transition. That case
dissolves, because a notifier does not need edge semantics:

```
edge-triggered    I saw working->blocked, so I alert.       Miss the message, miss the alert, forever.
level-triggered   the record says blocked and I last
                  alerted on working, so I alert.           Correct after any gap, restart or sleep.
```

- Level-triggered is how alerting is built everywhere, and it survives a
  session-watcher restart, a crash, the notifier's own restart and a closed
  laptop — none of which a log actually fixed, it only papered over.
- What it loses is a transition that happened **and was reverted** inside the
  gap: `blocked -> you answered it in the pane -> working`. That is exactly the
  alert you do not want.
- The per-subscriber state this needs — "what I last announced for this session"
  — belongs in the SDK rather than in each plugin (R24).

The log's other claimed uses are served without it: `tail --since` by verbose
logging in `session-watcher.log`, which is a debug surface making no schema or
retention promise; replay testing by a recording made deliberately (§A16), not
by an always-on log. What remains are a timeline view and time tracking, and
both are the analysis §A1 declares out of scope.

This refusal is load-bearing, not merely thrifty. Because nothing needs every
message, **coalescing is safe for every subscriber** (§A9.3), the handshake
needs no delivery policy (§A10.3), and R22 becomes a general rule instead of an
exception.

#### What a session does carry

The access pattern is asymmetric, and the design follows it:

- `message` — the last thing said — lives **in the record**, because every
  render of every session needs it.
- The previous ones live in a **per-session history read on demand**: a display
  wants them when somebody hovers or opens a preview, for one session at a time.
  Keeping them out of the record is what stops a delta growing from a kilobyte
  to twenty.
- It holds the last N messages **and** the last N state changes, both bounded by
  count in the configuration, both `0600`, both deleted with the session. So a
  picker can say "worked 40m, blocked 12m, and said these five things" without
  anything being retained once the session is pruned.

**We are not a transcript store.** That file is complete, it already exists, and
its format belongs to the agent — three reasons core should not duplicate it.

**[amended 2026-09-22 — D-76]** It is also no longer reachable from a record.
`agent_data.transcript_path` was how a display was meant to get there, and it
went with the rest of `agent_data`; no display had ever followed it in the three
months it was written on every hook. This is the one capability the removal
actually gave up rather than tidied, and it is written down here so that
whoever wants it back knows what they are asking for: a `transcript` field of
its own, argued for in §A7.4 like everything else, rather than a namespace to
hide it in.

#### What would bring the global log back

Wanting a genuine timeline across sessions, or time tracking that outlives them.
The session-watcher already sees every event, so it would be additive: an opt-in
append-only log with its own retention and its own privacy argument, plus a
cursor on the handshake. Nothing in the design forecloses it. Nothing in the
design pays for it today.

## A8. How we know an agent is dead

The hardest requirement in the system: hooks may never fire again — the terminal
was closed, the process was `SIGKILL`ed, the laptop slept — and the bar must
still be right.

### A8.1 Watch, do not poll

**[decided]** **[verified on macOS 26.6.2, 2026-09-16, and Linux 6.10.11,
2026-10-09]** Both kernels will tell you when a process you did not fork exits,
for a process owned by the same user, with no privileges or entitlements:

- **macOS**: `kqueue` with `EVFILT_PROC` / `NOTE_EXIT`. Verified with a probe
  registering on a sibling process' pid: registration succeeded and the event
  arrived the instant that process exited.
- **Linux**: `pidfd_open(2)` (kernel ≥ 5.3), the descriptors in one `epoll`
  set; a descriptor becomes readable on exit. Verified the same way, in Docker
  Desktop's VM: a `sleep` orphaned to pid 1 registered, was not readable while
  it lived, and was readable about 0.4ms after a `kill -9`; a pid already gone
  fails to register with `ESRCH`, as it does with kqueue.

So the session-watcher holds one watch per live session and learns about a `kill
-9` in milliseconds. The periodic sweep stays, but as a safety net rather than
the mechanism.

### A8.2 The sweep, and pid reuse

**[decided]** For anything not covered by a watch — a pid we failed to register,
a record inherited from a previous session-watcher — liveness is:

```
the recorded process is gone      -> the session has ended
we cannot tell, for any reason    -> touch nothing
```

The second line is the safety rail, and it covers a record with no process at
all, which can therefore never be proved dead. Not knowing must never become
deleting, or one unreadable moment wipes every live session from your bar.

"Gone" means `kill(pid, 0)` fails **or** the process start time no longer
matches the one recorded. Pid reuse is not hypothetical when records outlive a
week of uptime.

- **macOS**: `sysctl KERN_PROC_PID` gives `kinfo_proc`, whose `p_starttime` is a
  microsecond-resolution `timeval` at offset 0 of the structure. **[verified]**
  against `ps -o lstart` on 2026-09-16. The implementation uses the typed
  decoder in `golang.org/x/sys/unix` rather than hand-computed offsets.
- **[verified 2026-09-17 — D-29]** Three things that sysctl does not do the
  obvious way, each of which would have been a silent bug:
  - **A pid nothing is using fails with `EIO`, not `ESRCH`.** `EIO` normally
    means something quite different, so it is not enough on its own; `kill(pid,
    0)` is asked to settle it and only `ESRCH` from that counts as death.
  - **`p_comm` is a 17-byte buffer the kernel does not clear**, so what follows
    the terminator is whatever was there before — `"claude\x00\x00\x00sk"`.
    The name must be cut at the *first* NUL; trimming from the right leaves the
    garbage attached and every comparison fails in a way that looks exactly
    like the agent not being there.
  - **A zombie still has a `kinfo_proc`.** An exited process whose parent has
    not reaped it would be judged alive, so `SZOMB` is read as gone. We never
    fork an agent, so this is defence against somebody else's bookkeeping.
- **Start times are compared to the second, not exactly** (**[decided]** D-29).
  The available resolution differs by platform and by probe — microseconds from
  sysctl, clock ticks from `/proc` — so a record written by one and checked by
  another would disagree with itself. A second is safe because pid reuse needs
  the kernel's pid counter to wrap, which is tens of thousands of processes and
  not one second's worth.
- **The prober must be able to see the process asking.** A machine that answers
  about other pids but not about us is not describing this machine, and nothing
  it says may be acted on. Without that rail one broken moment ends every
  session on the bar at once. *(Taken from the Rust implementation, which got
  this right.)*
- **Linux**: `/proc/<pid>/stat` field 22, in clock ticks since boot, and kept
  that way rather than made a date (D-89). **[verified 2026-10-09, Linux
  6.10.11]** by M5's machine tests: a killed and reaped `sleep` is judged gone,
  and a record wearing this process's pid with the wrong start time is judged
  gone. The name in that line is in parentheses and may hold either, so it ends
  at the *last* `)`; a zombie is state `Z` and is read as gone, as `SZOMB` is.

### A8.3 Reboots

**[decided]** Every record carries the boot identity it was written under. On
startup the session-watcher ends every session from a previous boot without
probing anything. Without this rule a rebooted machine shows ghosts forever,
because those pids either do not exist or belong to something else entirely.

**[corrected 2026-09-17 — D-29]** On macOS it is **`kern.bootsessionuuid`**, not
`kern.boottime`. `kern.boottime` is derived as *(now − uptime)* and is therefore
adjusted by sleep and by NTP, so a boot identity built on it can drift — and a
drifting boot identity declares every session on the machine dead after a nap,
which is the single worst failure this system could have. `bootsessionuuid` is a
UUID the kernel mints once per boot and cannot drift. **[verified 2026-09-17]**:
`C89F9A5C-5A0C-4674-AF7E-BC8260851DC3`. Linux keeps
`/proc/sys/kernel/random/boot_id`, which was always a UUID.

### A8.4 Finding the agent's pid at all

**[decided]** The pid a hook knows is its own, not the agent's.
record-agent-event walks the parent chain from itself upward, bounded in depth,
recording each ancestor's pid, start time and executable name, and picks the
first whose name matches the agent binary the user configured. This is the one
job the `agent-integrations` section of the config does at runtime, and the
reason it exists.

The walk **must** happen in the hook process: by the time the session-watcher
reads the event the hook has exited and there is no longer a chain to walk from.

If no ancestor matches — a shell wrapper that re-execs, a renamed binary — the
chain is recorded anyway, the agent pid is marked unknown, and liveness falls
back to the lease (§A8.5). The hook never fails over this.

- **The nearest match wins**, which is what makes a claude session running
  inside a claude session resolve to the inner one: the session you are in.
- **[decided 2026-09-17 — D-29]** A configured `binary` matches three
  spellings: the short name the kernel reports, the executable's full path, and
  that path's basename. All three are things a person writes in a config file,
  and the absolute one is exactly how you disambiguate two claudes. The full
  path comes from `KERN_PROCARGS2` and is used for matching only — it is never
  stored, because a record is about a kilobyte and rides in every delta
  (§A13.1), and six absolute paths would be a quarter of that for something
  nothing reads yet.
- **The walk is bounded, and it remembers where it has been.** A process tree
  should never contain a cycle; the bound is there because "should never" is
  not "cannot", and the cost of being wrong is a hung agent (R1).

### A8.5 Idle is not dead

**[decided]** A lease — "no event for N minutes, assume gone" — is **never** the
primary signal. An agent waiting for your input fires no hooks for hours, and
that is precisely the state your bar most needs to keep showing. The lease
exists only for sessions whose process cannot be observed, which today means
none, and tomorrow means remote ones.

### A8.6 Ending is a transition, not a deletion

**[decided]** A dead session moves to `ended/` with a timestamp and stays
resumable for the retention window. Consequences that must be implemented, not
assumed:

- Displays receive `resumed` as a distinct fact from `created`; a bar that
  treats a return as a birth loses the session's age and history.
- **container-coordinates are discarded on resume.** Resuming in a different
  pane is the normal case, and a stale pane id silently breaks jump-to-session
  in the most confusing possible way: it takes you somewhere, and it is the
  wrong place.
- A session whose process is alive but which has been superseded inside that
  process (`/clear`) is ended, with a reason distinguishing it from a process
  that died.

### A8.7 Sleep

**[decided]** A sleeping laptop does not run your ticker, and wall-clock
arithmetic after a wake is wrong by the length of the sleep. Durations are
measured on a monotonic clock, and the session-watcher treats a wake as a reason
to re-scan everything rather than to trust anything it computed before.

## A9. The session-watcher

### A9.1 What it owns

- One process-exit watch per live session (§A8.1), and the sweep behind it.
- The reducer, for transitions it observes rather than is told about — which
  means death and supersession, never the passage of time (D-12).
- Deriving container-coordinates from captured-context, off the hook path.
- Coalescing and fan-out to displays.
- Spawning and supervising tool-integrations.
- Answering focus requests by driving containers in order.
- Reconciling from the store at startup, which is what makes every poke losable.

### A9.2 Singleness, detachment, lifecycle

**[decided 2026-09-17 — D-19]**

#### Who may start it

**Anyone.** record-agent-event spawns it when a poke finds nobody listening, and
a subscriber that loses its connection may do the same — it is the same library
call, and the lock below makes the race harmless.

**[decided 2026-09-17 — D-33] Which binary it spawns cannot be "this one".** The
hook library is linked into programs we did not write: inside
agent-notify-claude, `os.Executable()` is agent-notify-claude, and
`agent-notify-claude watcher run` is not a thing. So the binary is looked for in
this order — what `agent-notify-binary` names in the configuration, this
executable if it is actually called agent-notify, then PATH. PATH is last and
not trusted much, because a hook's PATH is not your shell's PATH, which is
exactly why the configuration key exists. **Not finding it is not fatal to
anything**: the record is already written and `list` still applies liveness as
it reads, so the system degrades to precisely what it was before there was a
session-watcher at all. Crash recovery therefore works
from both directions, and installation is one step: write the hook, and the rest
of the system assembles itself on the first event. A launchd agent or a systemd
user unit remains supported and documented for anyone wanting start-at-login,
through the identical code path.

#### How it stays single

- `flock(LOCK_EX|LOCK_NB)` on `session-watcher.lock`, held for the process's
  life. The kernel holds the mutex, so it survives every kind of death including
  `SIGKILL`, which a check-then-act on a pid file does not.
- The winner unlinks and rebinds the sockets. That is safe precisely because it
  holds the lock, and it means a leftover socket file from a killed
  session-watcher never blocks the next one.
- The lock file records pid and version for `doctor` to read, and is **never
  unlinked**. Deleting a locked file breaks the mutex: the next process creates
  a fresh inode and locks that instead, and then there are two session-watchers.

#### Detachment

record-agent-event starts the session-watcher as a child of the hook, so it
inherits four things from the agent, and all four are wrong for a process meant
to outlive it.

| Inherited | Why it is wrong | What we do |
| --- | --- | --- |
| the terminal session | closing that window sends a hangup to everything in the session, killing the session-watcher along with the terminal that happened to start it | `setsid` |
| **the hook's open pipes** | see below | stdin, stdout and stderr all pointed at the null device before the spawn |
| the working directory | the session-watcher pins the agent's directory forever: a disk cannot be unmounted, a deleted directory never goes away | `chdir("/")` |
| the agent's environment | a hook runs with the agent's variables, **API keys included**, and the session-watcher would hold them in memory for days | an explicit short list: `HOME`, `PATH`, the state-directory override, nothing else |

The pipe case is the one that will actually happen:

```
Claude hands the hook a pipe and waits for it to close as the signal the hook has finished.
The hook exits — but the session-watcher it started still holds that pipe, having inherited it.
The pipe never closes. Claude waits. Forever.
```

What a user sees is "the agent hangs after every tool call", with nothing in any
log pointing here, because nothing failed — something merely never ended.

#### A session-watcher that holds the lock but does not answer

record-agent-event does not care: it writes the store and exits, and the system
is eventually consistent (R1, R4). Diagnosis belongs to `doctor`, which reports
"pid N, version V holds the lock and does not answer on the socket", and
recovery is the explicit `agent-notify watcher restart`. **Nothing ever
auto-kills another process.** A notification tool that kills things unprompted
is not one you would leave running.

#### A running session-watcher older than the binary you just installed

No automatic stand-down either, for a concrete reason: with several core
versions installed at once (D-7), a session-watcher that yielded to any newer
poke would flap between two agent-integrations of different vintages. `doctor`
reports the skew; restarting is the user's word.

#### It does not exit when idle, with one exception

**[decided 2026-09-17 — D-33]** It stands down if its **state directory has been
deleted** under it. That is the one condition that ends this process without
being asked, and it is not an idle rule: a session-watcher whose store is gone
has nothing to watch, nothing to write and no way to be useful, and staying
would mean holding a lock inside a directory that no longer exists. Deleting the
state directory is also exactly how a person resets this system, and how a test
throws its temporary one away.

#### It does not exit when idle

A subscriber like sketchybar holds a connection all day, so an "exit when there
are no sessions and no subscribers" rule would almost never fire in a real setup
while still costing a state machine. The price is that an upgrade needs an
explicit `agent-notify watcher stop` or `restart`, and that is the right place
for the cost to land.

#### Signals and running it in the foreground

`SIGTERM` is a clean shutdown: stop the children it started, close the socket,
release the lock by exiting. `SIGHUP` is reserved for configuration reload (§A18
Q6). `agent-notify watcher run --foreground` skips the detachment entirely and
logs to the terminal, which is how it gets debugged.

### A9.3 Backpressure, and who may be coalesced

**[decided]** Each connected subscriber has a bounded queue. If it overflows,
the queue is emptied and replaced by a single **resync** marker: the subscriber
re-reads and is correct. Overflow degrades to a full redraw, never to a wrong
render.

This is also why record-agent-event must never block: a unix datagram socket
does not silently discard like UDP, it blocks the sender or returns `EAGAIN`, so
a slow display could otherwise reach all the way back and stall a user's agent.

**[decided — D-10]** Coalescing is universal. Changes to the same session merge
in the queue and no subscriber may opt out, because none needs to: a renderer
draws the current state, and anything acting on change compares what it last did
with what is true now (R22). A subscriber whose correctness depends on receiving
every message is built wrong, and §A7.7 explains why.

**[proposed]** Two further reductions, both free:

- The session-watcher **diffs** an incoming record against the one it holds and
  skips fan-out entirely when only stamps moved, so a fifty-tool-call turn does
  not become fifty renders.
- A subscriber declares **what wakes it**. A pane renamer does not care when
  `message` changes; "wake me only when `kernel` changes" is the common case and
  belongs in the handshake.

### A9.4 Supervising the integrations it started

**[decided 2026-09-17 — D-23]** The supervisor is a **reconciler inside the
sweep that already runs**, not a scheduler of its own. Each tick asks one
question per enabled integration — should this be running, and is it? — and
starts whatever is missing.

- **No exponential backoff.** Backoff exists to prevent restart storms, and a
  storm cannot happen when the fastest possible retry is one per tick. What it
  would have bought is already bought, and it would have cost a timer and a reset
  rule per child.
- **The reconciler makes configuration reload free.** An integration removed from
  the file simply stops being started; one added gets started on the next tick.
  No separate code path, and no way for the two to disagree.
- **The handshake is the health check.** A spawned child that has not connected
  within a grace period is presumed broken, killed, and counted as a failed
  attempt. Without this rule, children that hang before connecting accumulate
  forever.
- **One piece of state survives: a consecutive-failure count per integration.**
  "Consecutive" cannot mean "since the last handshake" — a child that connects
  and dies immediately would reset it forever — so it means *since the child last
  stayed connected for more than a minute*. A display that ran all day and then
  crashed starts again from zero; one that has never worked counts up and stops.
- **Give up after a configured number of attempts**, recording the reason. A
  binary that does not exist reaches that quickly, with the message naming the
  path that was looked at.
- **Giving up is never a dead end.** A configuration reload or
  `agent-notify watcher restart` retries everything, which is the same thing the
  user was going to do after installing the missing tool anyway.
- **A permanent refusal is not in this machinery at all**: a subscriber told no
  for a reason that will not change is never retried, so it must neither consume
  attempts nor reset counters. Version mismatch used to be such a refusal and no
  longer is (D-77) — versions are announced and logged, never arbitrated.
- **Connected but slow is degraded, not dead.** Its queue overflows and resyncs
  (§A9.3) and nothing else happens. We do not kill it, because nothing
  auto-kills (D-19).
- **Self-started subscribers are not supervised.** They are not our children; a
  disconnection is their own business and they may return whenever they like.
- **A child's death is learned for free**, since it is our child and its exit is
  reported to us rather than discovered by polling. The tick is not how we notice
  — only when we act, which bounds recovery at one tick.
- **`doctor` is the report**, and the log records *state changes* rather than
  attempts: "zellij failed after 5 attempts, binary not found at …" once, not on
  every retry. A supervisor that spams is a supervisor nobody reads.
- **Shutdown** is `SIGTERM` to the children, a short grace period, then
  `SIGKILL`.

## A10. The integration model

### A10.1 The constraint that decides everything

**[decided]** Go cannot load code at runtime. `plugin.Open` requires host and
plugin to be built by the identical toolchain against an identical dependency
graph; separately released repositories will never satisfy it. So "an
integration lives in its own repo" and "no process boundary" cannot both be
true. Composition is either at build time — the user compiles a binary
containing the integrations they chose, needing a Go toolchain and killing any
store of prebuilt integrations — or at run time, across a process boundary.

We take the process boundary. **But a process boundary does not have to mean
forking per event.**

### A10.2 The shape that results

**[decided]**

- **Every tool-integration is a long-lived process** that connects to
  `subscribers.sock` once and stays connected, declaring its capabilities on
  connect. One connection for its whole life: no fork per event, no compile-time
  coupling.
- **The session-watcher spawns and supervises them** as children — started once
  per session-watcher lifetime, restarted with backoff if they die, shut down
  cleanly with the session-watcher.
- **Every agent-integration is a binary that links the core library** and calls
  record-agent-event in-process. On the hook path there is no IPC at all beyond
  the poke.
- **Core ships as a library and a CLI.** A plugin author in Go puts the library
  in their `go.mod`, so *our* code is compiled into *their* binary and they
  never see the protocol. An author in another language speaks the socket
  protocol, or uses the CLI.
- **The CLI is a client, never a second implementation.** Anything it does, it
  does through the same library and the same session-watcher a plugin would use.

**[proposed]** There are **two lifecycles, not one**. The session-watcher spawns
and supervises what the configuration names, but it must also accept
**unsolicited connections** as a first-class case: a menu-bar app started by
launchd, an `agent-notify wait` in a shell script, a developer's harness run by
hand. The handshake is identical in both; only who started the process differs,
and the session-watcher supervises only what it started.

This supersedes the earlier "exec the display per event" decision (§A19 D-6).

**[amended 2026-09-19 — D-59]** "A display is told things and a container is
asked things" is the shape, and there is **one subcommand that belongs to
neither**. `capture-environment` is not a container's question: it is the one
thing that can only happen inside the agent's process tree, and any role can need
it. agent-notify-zellij-display holds a socket like a display and answers that
one subcommand, because a pane title cannot be painted without knowing which
pane. That was always possible; D-57 made it unavoidable by removing the
declarative alternative.

So capture is its own package rather than a corner of the container SDK. A
display imports `capture` and adds one case to the switch it already has; a
container declares `Capture` alongside its other three functions and
`container.Main` dispatches to the same place, so the two cannot answer
differently. The dichotomy stands: what makes a container a container is
`interpret-environment`, `focus` and `focused`, and those three are the ones the
container SDK is for.

### A10.3 Capabilities are declared, not configured

**[decided]** An integration announces what it is when it connects rather than
the user declaring it in three config sections that can disagree. What an
integration does is its own fact to state, and the user's file only has to say
that it should run at all.

**[amended 2026-09-18 — D-37]** This section used to add "zellij is both a
display and a container, and that is zellij's fact to state". That is no longer
true of any program here: a tool that plays two roles ships two programs, so in
practice an integration declares one role and the list stays open only for the
genuinely-two-at-once case nobody has met yet. The mechanism is unchanged —
roles are declared, never configured — and it is now load-bearing for a
different reason: the session-watcher learns which of two zellij programs is
present from what connects, not from what the config names.

A capability core does not know about is carried and ignored rather than
refused, so nobody has to teach core about a new kind of integration before
inventing one.

**[proposed]** The roles we can name today:

| Capability | What it does |
| --- | --- |
| `display` | renders session state where a human looks |
| `container` | places a session and brings it to the front (§A11) |
| `enricher` | annotates records without rendering or placing: git branch, project name, task id (§A7.4.1) |

#### The two halves of capture, and why they are two

**[decided 2026-09-17 — D-27]** Working out where a session lives is two jobs
with two different hazards, and they run in two different places for that
reason.

| | `capture-environment` | `interpret-environment` |
| --- | --- | --- |
| Where it runs | as a child of the hook, inside the agent's process tree | in the connected daemon, off the hot path |
| Why there | the agent's environment exists only inside the agent's process, so only a descendant can read it | because it is allowed to block |
| Contract | gather local, immediately available state: variables, a file, cwd. **Never talk to your tool, never open a socket, never wait.** | ask zellij which tab holds pane 7, ask aerospace which workspace the window is on |
| Returns | an opaque blob, stored under `captured_context.by.<name>` | coordinates, stored under `derived_context.<name>` |
| On failure | after `hook.captureTimeout` it contributes nothing, the hook writes and exits, the next hook tries again | that integration has no coordinates; everything else is unaffected (R13) |

- **A list of variable names is not enough**, which is what forced this shape. A
  capture may need to read a file, parse a socket path out of `$TMUX`, or look
  at cwd. It is arbitrary logic, so it is code, so it is a command.
  **[amended 2026-09-19 — D-57]** It is now not merely insufficient but gone: the
  list was the declarative half of §A11.4, and a second copy of a fact the
  integration already holds is a copy the user can break without being told. One
  form remains, and `capture-environment` is it.
- **The dangerous half is the one that talks to the tool**, and it is precisely
  the half that does not need to run in the agent's process tree. Splitting them
  is what keeps `tmux display-message` against a dead server — or any third
  party's equivalent — out of the path your agent waits on.
- **An author writes two functions and the SDK wires both to subcommands.**
  **[amended 2026-09-18 — D-38]** This used to wire interpretation to the socket
  and expose the subcommand only for testing. Both are now subcommands: a
  container is a program that gets run, not a daemon that stays connected, which
  removes a failure mode ("its daemon is down") that was indistinguishable from
  a legitimate state ("it never placed this session"). A stored blob can still
  be replayed through interpretation in a test with no zellij running — that is
  now the only path there is.
- **Capture is not on every hook.** An agent's environment does not change while
  it runs, so capture happens at session start, at resume, and whenever the set
  of integrations that have captured no longer matches the set enabled — a set
  comparison on each hook, and a spawn only when something is actually missing.
  **[2026-09-18 — D-39]** Which integrations to spawn is read from the config,
  because the hook has no socket to ask over: `capture-environment = true` in an
  integration's own table, written there by its own `install`.
  **[amended 2026-09-19 — D-58]** "A set comparison on each hook, and a spawn
  only when something is actually missing" is what this always said and is now
  what the code does; it used to spawn first and compare afterwards.
  That last clause is what lets a container you enable today place a session
  that started on Tuesday.
- **The blob is stored, not consumed.** The hook captures whether or not the
  daemon is running; an integration that was down interprets it when it connects
  (R4).

**[proposed]** The handshake carries more than the role list, because these are
all things the session-watcher must not guess:

- the **protocol version** it was built against — announced and logged, never
  negotiated on (D-77);
- **what wakes it** — which fields it cares about (§A9.3).

It carries no delivery policy and no cursor. Both existed to serve a subscriber
that needed every transition, and D-10 established that no such subscriber
exists.

**[decided 2026-09-17 — D-13] Writing into a session stays a non-goal, and
nothing needs doing to keep it possible.** The road is already built: a
container knows where a session lives and holds the tool binary, so "type this
into that pane" would be `zellij action write-chars` or `tmux send-keys` by the
same thing that already performs `focus`, over the same ordered, validated
chain. The capability list is open, so adding the verb would teach core nothing
new.

The three reasons it stays out, written down so the argument is not had twice:

- **It is not idempotent.** Focusing twice leaves you focused; sending "yes"
  twice types "yes" twice. Every retry and timeout policy that is safe for focus
  becomes dangerous.
- **It is a blind write into an unknown UI.** Nothing can see what state the
  agent's terminal is actually in, and answering a prompt that was answered a
  second ago does not vanish harmlessly — it types a stray character into a
  running session's composer.
- **It changes what the product is.** A bar that can click "allow" on `rm -rf`
  is an agent-control system wearing a notification system's clothes, and that
  deserves a deliberate decision rather than arriving as a convenience.

**The trigger for revisiting is not our courage, it is the agents**: when one
offers a *supported* control channel — an IPC socket, an API — all three
objections evaporate at once. Faking keystrokes never makes them evaporate.

**Commands beyond focus** are a different matter and remain open by
construction: open the transcript, kill it, whatever a picker wants to offer.
The open capability list already permits verbs core has never heard of, and none
of them writes into the agent.

### A10.4 What the library must give a plugin author

This is not a nicety; it is how promise 4 is kept. **[decided]** in intent, the
exact surface **[deferred]** to implementation.

- A single entry point that owns the whole lifecycle — connect, declare, receive
  the opening snapshot, subscribe, coalesce, reconnect with backoff, handle
  resync, shut down cleanly — so the author writes a render function and a
  `main` of about ten lines.
- Typed records and events with **versioned** decoding: a plugin built against
  v1 meeting a v2 session-watcher gets a refusal naming the mismatch, never a
  mis-parsed field.
- A store reader, so no plugin ever walks our directory layout itself. The
  layout is ours to change.
- Typed access to that integration's own config section, including resolving a
  `(kernel, detail)` pair to a glyph or colour with the user's overrides applied
  — otherwise every display reinvents theming and they all disagree.
  **[built in M10]** `Integration.Settings` decodes that one table into whatever
  shape the display declares and refuses an undeclared key *by name*, because a
  misspelled glyph that changes nothing and says nothing is the config bug
  people give up on. `session.Palette` resolves the pair, most specific
  first, and falls back for an unknown kernel to the glyph of the nearest known
  rank — which is the only place the urgency-rank of §A5.5 is actually cashed
  in. Both run inside the integration's own process, compiled in from the SDK,
  so R7 stays true of the session-watcher.
- **One way to read the agent's environment, whatever the integration is for.**
  **[built in M12, moved out of the container SDK 2026-09-19 — D-59]** The
  `capture` package is a constant and a `Main`: an integration adds one case to
  its switch and returns whatever it needs, and core stores it opaquely. It is
  not part of the container SDK because it is not a container's question — a
  display that paints a pane title needs it too.
- **A fake session-watcher that replays a scripted event stream**, so an author
  can `go test` a display without running an agent. Highest-leverage item on the
  list: there will be many integrations, and without this every one of them is
  tested by hand.
- **One way to run the tool the integration drives.** **[built 2026-09-21 —
  D-68]** The `tool` package is `Run`, `Summarise` and three error types. Every
  tool-integration shells out to something — zellij, aerospace, sketchybar —
  and the twelve lines that do it hold four mistakes that are each silent: a
  missing `WaitDelay`, which makes the timeout a lie; checking the error before
  `ctx.Err()`, which reports every timeout as `signal: killed`; collapsing "the
  binary is not there" into "the tool said no", which are opposite things a
  container must report differently; and summarising a tool's output with a
  hand-rolled ANSI strip, which puts §A15 outside core. Five copies existed,
  four in integrations and one in core, and they had already drifted apart on
  all four counts.
- **One way to say where agent-notify's files are.** **[built 2026-09-21 —
  D-69]** `Root` on the `Integration`, empty meaning "wherever this process's
  environment says", and every path question is a method on that struct. An
  integration that reads the environment variable itself is an integration
  taking the second of two routes into core's layout, and those two routes did
  not agree.
- **A wake-on the record cannot meet is refused at startup.** **[built
  2026-09-21 — D-70]** `Differs` matches field names by string, so a
  misspelled one never matches and the subscriber is never woken — while
  connecting, painting once off the opening snapshot, and reporting healthy
  for ever after. `Run` refuses it before the socket. The check is the SDK's
  and not the session-watcher's, because only the SDK can be sure an
  unrecognised name is a mistake rather than a field the record has since
  gained.
- **A way to find out what a display actually reads.** **[built 2026-09-21 —
  D-73]** `EachFieldMoved(base)` is one copy of a record per field, each
  differing in exactly that field. Every renderer in this system is a pure
  function of records, so a display can discover which fields its own output
  depends on and check that against what it asked to be woken for, instead of
  keeping the two in step by hand — which they were not (D-72).
- A conformance check an integration can run against itself.

**[decided]** The exported surface is deliberately narrow and everything else
lives under `internal/`, because every exported symbol is a semver promise to
repositories we do not control.

## A10.5 Versions and compatibility

**[decided 2026-09-17 — D-14]** Integrations live in repositories we do not
control and are not rebuilt when core ships, so a newer core will meet older
integrations and the reverse. Four rules were written here, and deliberately no
more: one version number for everything; additive-only within a major; across
majors refuse loudly and stay refused; and a binary that rewrites a record
preserves the fields it did not understand.

**[decided 2026-09-22 — D-77] None of them is in the code any more.**

They were never in force. **[clarified 2026-09-19 — D-57]** every rule was a
promise to repositories we do not control, and there are none: the module is
unpublished, `Version` is `0.0.0-dev`, and every integration reaches core
through a `replace` directive to a sibling directory. What D-57 left in place
was the *machinery* — a `schema` number, an unknown-field round trip, a major
check at the handshake — on the grounds that it would be wanted on publication
day. D-77 removed the machinery too, for the reason that decides the rest of
this document: **it was dead weight that had to be reasoned about on every
change**, and the one place it was actively wrong was that it resurrected fields
which had been deliberately deleted.

What that removed, precisely:

- **The `schema` number.** Nothing ever branched on it. "Versioned decoding"
  (R12) was a promise with no implementation anywhere in the tree.
- **The unknown-field round trip** (R28). `Record` kept a map of every key it did
  not recognise and wrote it back out on every save. It protected a record from
  binaries of different ages, which was real — and which also meant that a field
  removed on purpose was preserved for ever on every record that had ever
  carried it.
- **The major check at the handshake.** `Compatible` compared `major("0.0.0-dev")`
  with `major("0.0.0-dev")` and had never once said no. A subscriber still
  announces the version it was built against; the session-watcher logs it and
  acts on nothing.
- **"An unknown event is a newer adapter."** It is a mistake in an adapter, and
  the reducer still refuses to guess at it — for the reason in §A5.7, not for a
  compatibility reason.

What deliberately stayed, because it looks like version insurance and is not:

- **The stored urgency-rank and an unknown kernel being live** (R5, §A5.5). The
  unfamiliar kernel this protects is the one on a record already on disk, which
  outlives any binary; and `RankUnknown` has a second caller that has nothing to
  do with versions at all — `Urgency()` over an empty set of records.
- **The opaque namespaces**: `annotations`, `derived_context`,
  `captured_context.by`, and core carrying a detail string it never interprets.
  These are R7 and R10 — core knowing nothing about any agent or tool — and
  removing them would gut the design rather than trim it.

**What brings all of it back is publication.** The rules as written above are
still the right rules for a published module and this section is their
statement; M18 is where they become code again, together with the `Version` the
build has yet to stamp. Two things were considered and rejected when they were
first written, and are worth keeping on the record for that day:

- **Putting the major in the path and socket name** (`state/v1/`,
  `session-watcher.v1.sock`) so incompatible versions could never see each
  other. It would have removed the refusal path entirely, at the cost of
  version-littered paths for a case that should be rare.
- **Golden fixtures replayed in CI.** Which means **the additive-only rule was a
  discipline rather than a check, and breaking it failed silently.** That is why
  it is worth nothing until something enforces it.

`doctor` reports the core version embedded in every integration binary (§A16).
On an architecture where several core versions run at once — the consequence of
D-7 — that is the only way anyone diagnoses "why does codex show the wrong
state", and it is now the *only* thing that notices a mixed deployment at all.

**One consequence of D-8 worth stating**: the reducer runs in whichever binary
emitted, so two agents can reduce with different reducer versions. This is safe
only because kernel states are added and never redefined — an older reducer
yields a valid, less refined state, never an invalid one. That is a standing
constraint on every future reducer change, and it is the one compatibility
argument D-77 does not touch, because it is about two binaries running right now
rather than about a promise to a future one.

## A11. Containers: where a session lives, and getting there

### A11.1 Two questions, asked in two different worlds

- **Placing**: turning a captured-context into coordinates. Done by the
  session-watcher, after the fact, from the blob the hook captured. The
  *capture* runs in the hook because nothing else can read the agent's
  environment; the *interpretation* never does (§A10.3, D-27).
- **Focusing**: bringing a session to the front, on demand, when somebody
  clicked something. Done from wherever the click happened.

### A11.2 Nesting and order

**[decided]** Containers nest — a pane is inside a window — and the order is not
discoverable, so it is configuration: `order = ["aerospace-container",
"zellij-container"]`, naming integrations exactly as they declare themselves
(D-37). Focus
walks it outermost first, each step validated before it acts and required to
succeed before the next one is attempted. Displays have no such order because
they never interact.

### A11.3 Failure is typed, not generic

**[decided]** Coordinates are validated at the moment of use, never trusted from
the record: panes and windows die without telling us. A focus that fails
produces a named outcome — the pane no longer exists, the container is not
running, no container ever placed this session — because "the pane is gone"
should offer to prune the record while "aerospace is not running" should not.

### A11.4 A container is always a program

**[decided]** — and this section used to say the opposite, which is why it still
has a number. See D-57 for the reversal in full.

**What was here.** A container whose coordinates came from environment variables
and whose focus was one command needed no binary at all — a capture list and a
command template, and "add tmux support" was four lines of config rather than a
repository:

```toml
[integration.tmux]
capture = ["TMUX", "TMUX_PANE"]
focus   = ["tmux", "switch-client", "-t", "{TMUX_PANE}"]
```

**[removed 2026-09-19 — D-57]** Nobody ever wrote one. Both real containers ship
a binary, and the mechanism's only remaining effect was to keep `capture` in the
config file — which put a list of variable names, knowledge the integration
already holds in its own source, into a file the user is invited to edit. Editing
it breaks the integration *silently*: a capture naming the wrong variable
produces no error, just a session with no coordinates and a display that paints
nothing.

It was also structurally incomplete. A declarative container can never answer
`focused` (§A11.6), so a machine whose only container was declarative could never
get a better answer than `cannot-tell`; and one argv with no conditionals is not
enough for a correct tmux container anyway, which needs to select the window and
to attach when you are outside tmux.

So there is one way to capture and it is `capture-environment`, and the
integration is the only thing that ever names a variable. If the convenience of
four lines of config is ever actually wanted, it returns as a generic container
*outside* core — one small binary whose template lives in its own `settings`,
where it can also answer `focused` and run more than one command.

**[2026-09-19 — D-59]** One consequence of there being a single way to capture:
it stopped being a container's business at all, and moved to a package of its
own. A display needs it too.

**[amended 2026-09-18, having built it — D-44]** aerospace turned out to be a
coded container for a different reason than the removed section predicted. Its
capture is more than reading variables — it walks a process chain, which no list
of variable names can ask for — but its *interpretation* asks aerospace nothing
at all. The lookup it needs cannot be done in advance, because what it would
store is not a coordinate. See D-44.

### A11.5 container-coordinates live with what they were derived from

**[decided 2026-09-17 — D-27, replacing a proposal]** This section used to say
that container-coordinates are stored as `annotations.<container>`, so that one
write path served containers, enrichers and third-party taggers alike. That was
tidiness bought at the price of correctness, and it is what raised Q15.

Coordinates and a task label have **different lifetimes**. Coordinates were
computed from a captured context and are void the instant it is replaced; a task
label was derived from nothing of ours and nothing we do invalidates it. One map
holding both forces a provenance tag on every entry to tell them apart.

So coordinates live in `derived_context.<container>`, beside everything else
derived from the same snapshot, and are void with it (§A7.4.1). Containers still
get no private mechanism — they share that section with every other enricher,
under the same opacity guarantee (R7) — they simply do not share it with writers
whose data outlives the snapshot.

### A11.6 "Is this the session I am looking at?"

**[proposed]** A TTS or phone notifier must not fire for the session you are
already staring at, and a picker wants to mark the current one. Nothing in the
design tracks focus.

- Tracking it continuously would mean polling a window manager, and the answer
  would be stale the moment it was stored.
- So it is a **query, not a subscription**: the session-watcher asks the
  containers at the moment it matters. aerospace knows which window is focused,
  zellij knows which pane is.
- It is the mirror image of `focus`, and it belongs to the same capability.
- **A session is in front only if every layer agrees.** aerospace saying the
  window is focused while zellij is absent is not a yes, it is unknown — see
  §A11.7.

### A11.7 When no container can answer

**[decided 2026-09-17 — D-18]** Containers are optional, and a fresh install has
none. The plan has to say what that install does, because it is the default and
not an edge case.

**Without any container, everything works except navigation.** States, ranks,
counts, liveness, death detection, chips, `wait`, `list` and history touch no
container at all. What dies is exactly `focus-session`, the picker's jump, and
the focused query. agent-notify degrades to a working read-only semaphore, which
is a reasonable thing to ship and a reasonable thing to run forever.

**Every capability query is three-valued**: yes, no, and *nobody can tell*
(R27). The third answer is the design's responsibility, and each consumer names
its behaviour for it:

| Query | Nobody can tell |
| --- | --- |
| is this session in front? | treat as **not** in front, and notify. Silence would mean a user with no containers never receives a notification, which is worse than the occasional redundant one |
| where does this session live? | no container-coordinates are recorded; the session is complete without them |
| bring this to the front | a typed failure naming which layer could not answer (§A11.3) |

**A display should be able to ask whether focus is available at all**, per
session, so that a chip does not offer a click that can only produce an error.
It is cheap — it is "does this record carry any place" — and it separates two
situations that feel identical and are not: no container is configured, versus a
container exists but never placed *this* session because it started outside
zellij.

## A12. Displays

### A12.1 The catalogue, and what each display demands

**[proposed]** This table is the derivation of §A7.4, kept because it is the
argument for every field: run the design backwards from displays we can actually
picture, and take the union of what they must read. A field no imagined display
reads does not exist.

| Display | Must read | What only it forces |
| --- | --- | --- |
| sketchybar semaphore | kernel, count per kernel, name, message, `state_since`, key | — |
| zellij pane and tab titles | kernel, name, **the zellij place** | a tab glyph is `max(rank)` over its panes: a **total order** must be core-owned (§A5.5) |
| zellij picker | the above plus cwd, agent, age | sorting by urgency-rank then `state_since` |
| macOS / phone / TTS notifier | current state, against what it last announced | level-triggering, so it survives any gap (R22); "am I focused?" (§A11.6) |
| tmux or menu-bar counters | kernel only | a self-starting subscriber (§A10.2) |
| statusline inside an agent | everything, cheaply, with no session-watcher | the cold file read path (§A7.6) |
| LED, Stream Deck, physical light | one aggregate | `max(rank)` over everything |
| "focus my most urgent agent" | rank, `state_since`, key | nothing new |

Two requirements exist **only** because of this exercise: the total urgency
order is core-owned, and one subscriber needs the transition rather than the
state.

### A12.2 Resync is a first-class path

**[decided]** A display must be able to arrive at any moment and be correct:

- On connect, the session-watcher sends a full snapshot before any delta.
- A display may ask for a snapshot at any time — `sketchybar --reload` restarts
  a whole bar, and a display that cannot ask ends up blank until an agent
  happens to do something.
- Queue overflow produces the same snapshot path (§A9.3).

### A12.3 Displays are not trusted with correctness

**[decided]** A display may be slow, may crash, may be killed by the user, may
be a shell script. None of that may affect the agent, the store, the
session-watcher, or another display. Its failures are logged and its connection
is dropped and retried; nothing waits for it.

## A13. IPC

**[decided]**, with the transport choices verified on this machine.

| Path | Transport | Why |
| --- | --- | --- |
| record-agent-event → session-watcher | `SOCK_DGRAM` on `session-changes.sock` | connectionless: no handshake cost in a process that must exit in milliseconds, and nothing to clean up if the session-watcher is absent. |
| session-watcher ↔ integration | `SOCK_STREAM` on `subscribers.sock` | long-lived, per-connection state, a handshake, and detectable backpressure. |

- **[verified 2026-09-16]** `SOCK_SEQPACKET` on `AF_UNIX` — which would have
  given message framing *and* reliability — **is not supported on macOS**
  (`net.Listen("unixpacket")` fails with `protocol not supported`; Go supports
  it on Linux only). So the stream carries **newline-delimited JSON**: JSON
  escapes real newlines, so the framing is unambiguous, and the protocol stays
  readable with `nc` when something is wrong.
- **[verified 2026-09-16]** macOS caps a unix datagram at
  `net.local.dgram.maxdgram` = **2048 bytes**, with a 4096-byte receive buffer.
  This is not a limitation to work around; it is a reason the poke carries no
  content. The poke is `{key, sequence}` and the store holds the rest.
- The session-watcher's receive loop must never do work inline: read, mark
  dirty, return. A 4KB receive buffer is eight pokes deep, and eight agents
  taking a turn at once is a Tuesday.
- record-agent-event sends with a short write deadline and treats a timeout as
  "the session-watcher is busy, it will reconcile" — never as an error worth
  telling the user about.

### A13.1 What crosses each link

**[proposed — resolves Q4]** The question was whether a delta carries the event
or only "this session changed, re-read".

| Link | Carries |
| --- | --- |
| record-agent-event → session-watcher | `{key, sequence}` and nothing else. The 2048-byte cap is not an obstacle to work around; it is the rule "events are hints" enforced by the kernel. |
| session-watcher → subscriber | the **whole record**, plus `previous_kernel` and the event that caused it. |

- The stream has no 2 KB limit and a record is about a kilobyte, so sending it
  costs nothing and takes the store off the hot read path entirely.
- This does not create a second source of truth: the record on the wire carries
  its `sequence`, a subscriber applies it only if it is newer, and the store
  remains the recovery path for a cold start or a gap (R4, R16).
- `previous_kernel` saves a subscriber from re-examining a record when nothing
  it cares about moved. It is an optimisation, never the mechanism: correctness
  rests on comparing what was last done with what is true now (R22), not on
  having watched the transition go by.

## A14. Configuration

**[decided]** TOML, at `~/.config/agent-notify/config.toml`.

```toml
keep-ended-sessions = "168h"   # seven days, ParseDuration's spelling (D-78)
history-messages    = 20       # per session, by count; 0 keeps none
history-changes     = 100

[agent.claude]
binary = "claude"              # matched against the process ancestry (§A8.4)

[integration.zellij-display]   # one tool, two roles, two programs (D-37)
enabled             = true     # absent means true; writing the table is the ask
binary              = "agent-notify-zellij-display"
capture-environment = true     # the hook runs it; WHAT it reads is its own (D-57)
settings = { glyphs = { working = "*" } }   # opaque to core, handed through

[integration.zellij-container] # installed separately, and optional
binary              = "agent-notify-zellij-container"
capture-environment = true     # each integration captures for itself, so either
                               # can be installed without the other

[container]
order = ["aerospace-container", "zellij-container"]
```

**[decided 2026-09-29 — D-85] Beside the file is `conf.d/`, one file per
integration, written by that integration's `install` and read before the
user's file, which wins.** A drop-in holds what only the program can know: the
absolute path of its binary and of the tool it drives, an agent's process
name. **[amended 2026-10-07 — D-87]** It used to hold two more — the signing
identity a bundle carried and the launch agent that kept a display alive — and
there is no longer a bundle or a launch agent anywhere in this repository to
describe. Nothing in `conf.d` is the user's to maintain and nothing in
`config.toml` is a program's to write.

**Everything in `config.toml` is the user's to write, and nothing in it is an
integration's.** **[decided 2026-09-19 — D-57; extended from the file's contents
to the act of writing it 2026-09-20 — D-66]** That is what the file is *for*,
and it was not true until D-57: `capture` named the variables an integration
reads, which the integration already knows and the user cannot get right. The
test for whether a key belongs here is whether only the user can know the
answer — their agents, their tools, their retention, their glyphs. A key whose
correct value is a fact about a program is that program's business, and putting
it here only creates a second copy that can disagree with the first, silently.

`capture-environment` is the one survivor of that test and it survives narrowly:
the hook has no socket and cannot ask (D-39), so it must read *somewhere* whether
to run this binary. One boolean, written by an `install`, whose wrong value fails
coarsely — not a list of strings whose wrong value fails by painting nothing.

Rules that shape it:

- **One table per integration**, not three. The earlier sketch named zellij in
  `tool-integrations`, `display-configuration` and `container-configuration`;
  that is three places to keep in sync and a fourth failure mode when they
  disagree. **[clarified 2026-09-18 — D-37]** zellij now appears twice, and this
  rule is intact rather than bent: the table is keyed on the **program that
  runs**, not on the role it plays. Two tables because two binaries, and neither
  describes the other, so there is nothing to keep in sync. Install one and
  there is one table.
- **`settings` is opaque to core** and parsed by the integration that owns it.
  Core must not know sketchybar's colour keys. **[amended 2026-09-17 — D-24]**
  The sketch above used to put glyphs in a separate `[display.zellij.glyphs]`
  table, which broke both this rule and the one above it: glyphs *are* an
  integration's settings, and naming zellij twice is the three-places problem
  with one place removed rather than solved. `[container.tmux]` moved to
  `[integration.tmux]` for the same reason, which also leaves `[container]`
  holding nothing but `order` — policy that belongs to no single integration.
  Every tool now appears exactly once, and `[integration.<tool>]` decodes as a
  uniform map of one struct.
- **Roles are not configured** — they are declared (§A10.3). Container *order*
  is configured, because nesting is not discoverable.
- **Every timeout and every duration has exactly one name here**, and a defined
  behaviour on expiry. **[decided 2026-09-17 — D-20]** Three rules about the
  file itself:

- **One config, never layered.** No per-project file. Everything in it is global
  — one bar, one set of containers, one glyph table — and per-project settings
  would only make sense for per-session values, of which there are none.
- **Reload re-reads on `SIGHUP`**, sent by `agent-notify watcher reload`. No
  file watching: picking up a half-saved file mid-write is a real failure, not a
  theoretical one. Values that cannot be changed under a running process —
  socket paths, the state directory — say so rather than pretending to apply.
  record-agent-event has no staleness to worry about, being a fresh process on
  every hook.
- **One environment override, `AGENT_NOTIFY_ROOT`.** State and runtime normally
  live in different places, one durable and one temporary; when this is set,
  both go beneath it. One variable is what makes "run an isolated instance" a
  single step, which two would not.

And the rule that outranks the other three: **a missing or malformed config must
never break the agent.** record-agent-event falls back to defaults, logs, and
exits 0 (R2).

## A15. Security and privacy

**[decided]** The system reads an agent's environment and stores what an agent
said. Both deserve care.

- **What is stored is what an integration returned, and nothing else.**
  **[restated 2026-09-17 — D-27, simplified 2026-09-19 — D-57]** Core spawns the
  integration's `capture-environment` as a child of the hook, so it inherits the
  agent's environment in full and returns an opaque blob — core stores the blob.
  There is no longer a second, declarative form in which core read named
  variables itself, so this is the whole rule rather than half of it: what
  reaches the disk and the status bar is bounded by what the integration chose
  to hand back, not by what it could see, and never by what the config file
  happened to name.
- **Which makes an enabled integration a trust boundary, exactly like the config
  file.** It runs as you, it was named in a file you wrote, and it can read the
  environment of the agent it was spawned under. That is the same boundary a
  shell rc file draws, stated so it is chosen rather than discovered.
- **Everything is `0700`/`0600` and per-user.** No world-readable path anywhere.
- **Stored message text is bounded, control characters stripped, newlines
  collapsed.** A display will interpolate it into a shell command or a bar item;
  an agent's output is attacker-influenced text in the general case.
- **The config file is trusted**: the session-watcher spawns the binaries it
  names. That is a deliberate, documented trust boundary, no different from a
  shell rc file.

**[decided — D-10]** Refusing the global event log is a privacy decision as much
as an architectural one. What this system holds is bounded by the sessions that
currently exist plus the retention window: nothing accumulates, and pruning a
session takes its messages with it. Session history (§A7.7) does not change that
— it is count-bounded and dies with its session — and its bound should still be
settable to zero.

## A16. Observability and development

**[decided]** in intent; details with implementation.

- `agent-notify tail` — watch events as they flow, in a terminal.
- `agent-notify list` — what is running, most urgent first. **[decided
  2026-09-17 — D-30]** Two forms, not one: a human table by default and
  `--json` for scripts and for a display that wants a snapshot without being a
  subscriber. Choosing the form by whether stdout is a terminal was rejected —
  a command that behaves differently when you pipe it is a command you cannot
  reason about from its own documentation.
- `agent-notify doctor` — is the session-watcher up, is the socket bound, which
  integrations connected, which agents have hooks installed, does the config
  parse, do the paths fit in 104 bytes.
- `agent-notify install <agent>` — idempotently register the hooks in that
  agent's own settings file, with `--print` to show what it would write. This is
  real product surface, not a footnote: an integration nobody can install is not
  distributed.
- A recorded event stream that can be replayed into any display, shared with the
  SDK's fake session-watcher (§A10.4).
- Both processes log to `agent-notify.log`, named after neither because both
  write it: the session-watcher logs its state changes, and record-agent-event
  logs there because a hook must never write to stdout (§A17 R2). Every line
  carries the `component` that wrote it, or the file cannot be read.

**[proposed]** Three more entry points, each demanded by something above rather
than invented here:

- `agent-notify annotate <owner> <json>` — the third-party write path of
  §A7.4.1, and the same one containers use for places.
- `agent-notify history <key> [--messages N] [--changes N]` — the on-demand read
  of §A7.7, and what a hover or a preview calls.
- `agent-notify record > fixture.jsonl` and `agent-notify replay fixture.jsonl`
  — a recording made deliberately, which is what replay-based testing needs
  instead of an always-on log.
- `agent-notify wait --until <kernel> [--session <key>]` — blocks until a
  session reaches a state, then exits. It is a subscriber like any other, it
  costs almost nothing given the socket, and it is what makes the whole system
  scriptable: chaining agents in a shell pipeline, gating a deploy on a review
  finishing. Of everything in the sweep, this is the application that most
  validates the architecture.

## A17. Rules

The invariants. Any change that breaks one of these is a change to this
document, not a bug fix.

- **R1** — The hook path never blocks on anything it does not control.
- **R2** — record-agent-event never fails the hook and never writes to the
  hook's stdout. An agent may interpret both; Claude reads hook stdout into the
  session and treats some exit codes as instructions. Diagnostics go to the log.
- **R3** — No integration code runs on the hook path. The hook captures; the
  session-watcher interprets.
- **R4** — The store is the only truth. Every message on every socket is a hint
  that may be lost without consequence.
- **R5** — Not knowing must never become deleting.
- **R6** — Death is a transition, not a deletion; a session that ends is filed
  away, and one that returns is recognised.
- **R7** — Core never interprets a detail string, an annotation, an
  integration's settings, or an integration's stored section.
- **R8** — A display must render correctly knowing only the kernel state.
- **R9** — Presentation policy lives in the user's configuration, never in an
  agent's payload.
- **R10** — Core contains no name of any tool. Names of agents appear only where
  the user's config put them. **[amended 2026-09-20 — D-65]** With one
  exception, `hook/internal/branch`, and the exception is argued rather than
  excused: the rule exists so per-tool knowledge lives in the integration that
  owns the tool, and there is no integration that owns the checkout every agent
  runs in (§A7.4.4).
- **R11** — Core stores nothing out of the agent's environment that an
  integration did not choose to return, and an integration sees only the
  environment it was started in. **[restated 2026-09-17 — D-27]** The earlier
  form, "nothing leaves the environment that the configuration did not name",
  stopped being true the moment a capture became arbitrary logic: a child
  process inherits the environment, and it cannot be scrubbed without removing
  the thing it exists to read. The boundary is now the same one the config file
  already draws — you chose the binary and you enabled it (§A15).
  **[2026-09-19 — D-57]** The hedge in that restatement is gone with the
  declarative capture: there is no longer a form in which core reads named
  variables itself, so the rule is true as first written rather than true of one
  half and restated for the other.
- **R12** — *Retired 2026-09-22 by D-77, and due back on publication day.* It
  read: within a major version everything is additive and nothing negotiates;
  across majors the handshake refuses and names what to upgrade; and nothing is
  ever deleted merely because it could not be understood (§A10.5). The last
  clause survives on its own terms for a stored record's kernel — see R5 — and
  the rest is not in the code.
- **R13** — An integration's failure degrades that integration only: never the
  agent, never the session-watcher, never another integration.
- **R14** — The CLI is a client of the library and the session-watcher. There is
  never a second implementation of anything.
- **R15** — A backlog that cannot be delivered degrades to a resync, never to a
  wrong render.
- **R16** — Ordering that matters is a sequence number in the store, never an
  assumption about the transport.
- **R17** — Coordinates are validated at the moment of use.
- **R18** — Every timeout is named, in one place, with a defined behaviour on
  expiry. **[amended 2026-09-20 — D-62]** The name is in the source, beside what
  it bounds, not in the configuration file: a timeout is mechanism, and the only
  durations a person is asked about are the ones they have a real opinion on.
- **R19** — Durations are measured on a monotonic clock; stored and displayed
  times are UTC.
- **R20** — Extension never requires editing core.
- **R21** — Every display-visible vocabulary ships a numeric urgency-rank beside
  its name, so that an unfamiliar value can be sorted and rendered rather than
  dropped. Enabling that is core's job; guaranteeing it looks right is not.
- **R22** — No subscriber's correctness may depend on having received every
  message. Anything acting on change acts on the difference between what it last
  did and what is true now.
- **R23** — A subscriber declares what wakes it, so nothing is woken for a
  change it does not care about.
- **R24** — Anything every display would compute identically is computed once,
  in the SDK: the name fallback, the aggregate urgency-rank, the ordering. A rule
  reimplemented per display is a rule that will differ per display.
- **R25** — Where the writer is known, core stamps who wrote it; a writer never
  names itself. Identity that can be asserted is identity that will be asserted
  wrongly, and by accident before ever by malice.
- **R26** — If a fact is derivable identically by every subscriber from the
  record it already holds, it is not a state change; it is rendering. Elapsed
  time is the standing example.
- **R28** — *Retired 2026-09-22 by D-77, and due back on publication day.* It
  read: a binary that rewrites a record preserves the fields it did not
  understand — additive-only protects readers; only round-tripping protects the
  record. True, and the reason it went is that it also protects a field somebody
  deliberately deleted, which is what D-76 found when it tried to remove three.
- **R27** — Every capability query has three answers: yes, no, and nobody can
  tell. The behaviour on the third is part of the design, never an oversight,
  because it is the state of every fresh install.

## A18. Open questions

Numbers are stable: a question that gets answered stays here, marked, rather
than disappearing and taking the reasoning with it.

### Resolved

- **Q17 — where does an integration's log go?** Answered 2026-09-18 in M13: a
  supervised child's stderr is relayed into agent-notify's log, line by line,
  tagged with its name. A child started by a daemon has no terminal to complain
  to, and a display whose failure is invisible is a display nobody can fix. One
  started by a person still writes to the terminal they started it in.
- **Q3 — the read path.** Resolved in §A7.6: the session-watcher serves the
  snapshot from memory, there is no snapshot file, and the file walk survives
  for cold reads and for a statusline with no session-watcher.
- **Q4 — what a delta carries.** Resolved in §A13.1: the whole record plus
  `previous_kernel` and the event.
- **Q10 — does the event log survive?** Answered 2026-09-17, D-10: no global
  log; yes to per-session history bounded by count (§A7.7). The reasoning is
  that a notifier is level-triggered, which removes the only in-scope subscriber
  that needed every transition.
- **Q11 — annotation ownership.** Answered 2026-09-17, D-11: derived, never
  policed. A connected integration cannot name an owner at all; the CLI may name
  any; lifecycle follows provenance (§A7.4.1).
- **Q12 — does the session-watcher need a scheduler?** Answered 2026-09-17,
  D-12: no time-driven state changes at all. A display holds `state_since` and
  may render elapsed time however it likes; the state itself never moves because
  the clock moved.
- **Q13 — writing back into a session.** Answered 2026-09-17, D-13: it remains a
  non-goal, nothing in the design needs changing to keep it reachable, and the
  condition for revisiting is an agent offering a supported control channel
  (§A10.3).
- **Q14 — what is versioned, and what refuses.** Answered 2026-09-17, D-14: one
  version number, additive-only within a major, refusal across majors, and the
  urgency-rank as an enabler rather than a guarantee (§A10.5). *Reopened and
  answered again 2026-09-22, D-77: nothing is versioned and nothing refuses,
  because nothing is published; the answer above is what comes back on
  publication day, and the urgency-rank clause never left.*
- **Q1a — is `broke` its own kernel?** Answered 2026-09-17, D-15: yes. Only
  kernels carry a count and an urgency-rank, and a failed turn needs both.
- **Q1b — does focus act as a read receipt?** Answered 2026-09-17, D-16: no.
  Nothing marks a finished turn as read; it waits until you reply.
- **Q1c — one `message` field or several?** Answered 2026-09-17, D-17: one,
  overwritten by whatever event last carried text.
- **Q2 — the session-watcher's life.** Answered 2026-09-17, D-19: anyone may
  start it, a kernel-held `flock` makes it single, four inheritances are cut at
  spawn, a wedged or outdated session-watcher is reported and never auto-killed,
  and it does not exit when idle (§A9.2).
- **Q9 — does the session-watcher exit when the last session ends?** Answered by
  the same: no.
- **Q5 — are subagents sessions?** Answered 2026-09-17, D-21: no. A finished
  subagent maps to `agent-progressed` and the parent goes on showing as working.
- **Q6 — config layering, reload, overrides.** Answered 2026-09-17, D-20: one
  config, `SIGHUP` to re-read, one `AGENT_NOTIFY_ROOT` (§A14).
- **Q7 — component names.** Answered 2026-09-17, D-22: renamed throughout, with
  the wire format and the command line kept short on purpose (§A2.1).
- **Q8 — supervision policy.** Answered 2026-09-17, D-23: a reconciler in the
  existing sweep, one retry per tick, a consecutive-failure count, and giving up
  that a reload undoes (§A9.4).
- **Q15 — how does an annotation know where it came from?** Raised 2026-09-17
  while building M3, answered the same day by D-27: it does not, because the
  question dissolved. Annotations were carrying two different lifetimes in one
  map, and the fix was to stop doing that rather than to tag each entry. A
  record now has `captured_context`, `derived_context` and `annotations`, and
  which one a write lands in is decided by how it arrived (§A7.4.1).

### Open, and blocking

None. Every question that decides what a record contains is answered, so the
meta-plan can be compiled.

### Open

- **Q16 — does an integration need core's `Duration`?** **[raised 2026-09-18
  building M10, answered 2026-09-18 building M15 — no]** Four integrations now
  have the setting and all four are the same setting: a per-invocation
  `timeout`, spelled `"2s"`, parsed with `time.ParseDuration`. What they share
  is a pattern, not a need for the type. Core's `Duration` exists to understand
  `7d`, and a timeout measured in days is not a thing anybody wants — the one
  place `7d` belongs is `keep-ended-sessions`, which is core's own setting and
  not an integration's. So it stays internal. If an integration ever wants a
  setting measured in days, exporting it then is still additive.

  **[overtaken 2026-09-20 — D-62]** The question dissolved rather than being
  answered differently: those four `timeout` settings are gone, core's
  `Duration` is gone with them, and the one duration left in the file is a
  `time.Duration` written in nanoseconds. The reading that held up is the one
  about what a timeout *is* — a pattern the integrations shared, not a
  preference any of them was asking a person about.

  **[reopened and answered yes 2026-09-27 — D-78]** Not because an integration
  wants day units, but because a person should not have to know which table they
  are in to know how to write eight seconds. `session.Duration` is exported;
  the two bars that still have a duration import it.

Nothing is open as of M15, which is a statement about this moment rather than a
milestone: building raises more, and each gets a number here and an entry in
§A19 when it is settled.

## A19. Decision log

Newest last. A decision that was reversed stays here with its reason; the value
of this section is that it prevents re-litigating.

- **D-1** (2026-09-16) — Three repo families: core, one repo per agent, one repo
  per tool. Distribution, not performance, is the reason the rewrite exists.
- **D-2** (2026-09-16) — Liveness is an event-driven process-exit watch, with a
  sweep as the safety net, `(boot-id, pid, start-time)` as the identity, and
  "cannot tell → touch nothing" as the rule. Verified on this machine.
- **D-3** (2026-09-16) — The store is the truth; socket messages are hints.
- **D-4** (2026-09-16) — Container metadata is **not** gathered on the hook
  path. The hook captures environment and ancestry; the session-watcher
  interprets. *Supersedes* the original sketch in which record-agent-event
  called every container's gather-metadata synchronously, which put two to five
  external command invocations inside every hook the user's agent waits on.
- **D-5** (2026-09-16) — State is a closed, core-owned kernel plus a free-form,
  agent-owned detail; the kernel is defined by what the state demands of the
  human. Self-describing states carrying their own glyph and urgency were
  considered and rejected.
- **D-6** (2026-09-16) — Tool-integrations are long-lived processes holding one
  socket connection. *Supersedes* the earlier agreement that displays should be
  exec-per-event; the reason for the reversal is the requirement that we never
  shell out per event and that plugin authors link the library instead.
- **D-7** (2026-09-16) — Core ships as a Go library plus a CLI; the library is
  the plugin SDK. Go's runtime plugin loading is unusable for separately
  released repos, so the process boundary stays and the library removes the
  protocol from the author's view rather than removing the boundary.
- **D-8** (2026-09-16) — The reducer lives in core and runs in two places:
  record-agent-event for agent-caused transitions, the session-watcher for
  observed ones.
- **D-9** (2026-09-17) — Repository layout: every core and every integration is
  an independent repository with its own `.git`, `.tool-versions` and dotfiles;
  `agent-integrations/` and `tool-integrations/` are containing directories on a
  development machine and nothing more; this document lives in the core repo.

- **D-10** (2026-09-17) — No global event log. A notifier is level-triggered, so
  nothing in scope needs every transition; coalescing is therefore universal,
  the handshake carries no delivery policy, and R22 generalises instead of
  carving an exception. What survives is per-session history, bounded by count
  and deleted with its session, because a display showing more than the last
  message is a content question and not a history question. Reverses the
  event-log proposal made earlier the same day.

- **D-11** (2026-09-17) — Annotation ownership is derived, not enforced: a
  connected integration cannot name an owner because the session-watcher stamps
  the one it declared at handshake, while the CLI may name any, since a human at
  a command line is the user. Lifecycle follows provenance — invited annotations
  die with the captured-context they were derived from, unsolicited ones
  persist. There is no boundary to enforce here, so enforcement would have been
  theatre.

- **D-12** (2026-09-17) — Elapsed time never changes a state. The
  session-watcher's sweep keeps doing lifecycle work — death, pruning — and
  nothing else; a display holds `state_since` and is free to make a long wait
  look however it likes. The one candidate that would have been genuinely
  semantic, decaying a finished turn to idle, is the same problem as Q1b and is
  answered there rather than twice.

- **D-13** (2026-09-17) — Writing into a session stays a non-goal. Not because
  it is hard — the container already holds the coordinates and the binary — but
  because it is non-idempotent, it is a blind write into a UI whose state cannot
  be seen, and it turns a notification system into an agent-control system. The
  trigger for revisiting is an agent offering a supported control channel, not a
  decision on our side to fake keystrokes.

- **D-14** (2026-09-17) — One version number for everything, additive-only
  within a major, a loud and permanently un-retried refusal to run any
  tool-integration from another major, and the urgency-rank offered as an
  enabler of graceful degradation rather than a promise of it. Path-and-socket
  versioning was rejected as clutter for a rare case, and golden-fixture
  compatibility tests were rejected, which leaves additive-only a discipline
  rather than a check.

  **[amended 2026-09-22 — D-77]** Every clause but the last is out of the code
  until the module is published. The urgency-rank clause stays, and stays for a
  reason that was never about versions: what it degrades gracefully for is a
  record on disk carrying a kernel this build has been taught to forget.

- **D-15** (2026-09-17) — `broke` is its own kernel state, not a detail on
  `finished-a-turn`. Details carry neither a count nor an urgency-rank, and a
  failed turn needs to be counted on its own and to sort above a completed one.
  The six states of §A5.6 are settled.

- **D-16** (2026-09-17) — No read receipt. `finished-a-turn` persists until the
  user replies, and neither focus nor elapsed time clears it. Focus-as-receipt
  would fire only for navigation that goes through agent-notify, and a rule that
  fires unpredictably is worse than none; sampling the focused window would make
  a core state depend on a container's capability. The bar accumulating is
  accepted, on the grounds that it accumulates because unanswered agents do.

- **D-17** (2026-09-17) — `message` is a single field overwritten by whatever
  event last carried text, because the kernel-and-detail pair already says how
  to read it and the per-session history already serves depth. Its text may
  therefore be older than the state it sits beside.

- **D-18** (2026-09-17) — Capability queries are three-valued, and a fresh
  install with no containers is the default case rather than an edge one.
  Without any container everything works but navigation; "nobody can tell"
  whether a session is in front means notify rather than stay silent; and a
  session counts as in front only if every layer that placed it agrees.

- **D-19** (2026-09-17) — The session-watcher's life. Anyone may start it,
  including a subscriber that lost its connection, so recovery works from both
  sides and installation stays a single step. A kernel-held `flock` on a
  never-unlinked lock file makes it single. The spawn cuts four inheritances —
  terminal session, the hook's pipes, the working directory, the agent's
  environment — and the pipe one is the bug that would otherwise hang the user's
  agent with nothing in any log. A session-watcher that is wedged, or older than
  the binary just installed, is reported by `doctor` and never auto-killed or
  auto-restarted, because several core versions legitimately coexist (D-7) and a
  tool that kills processes unprompted is not one to leave running. It does not
  exit when idle.

- **D-20** (2026-09-17) — One config file with no layering, re-read on `SIGHUP`,
  and one `AGENT_NOTIFY_ROOT` relocating both the state and runtime directories.
  A broken config never breaks the agent.
- **D-21** (2026-09-17) — Subagents are not sessions. A finished one is progress
  by its parent, which is what the core-side reducer made possible (D-8).

- **D-22** (2026-09-17) — Renamed: emit becomes record-agent-event, the daemon
  becomes the session-watcher, a place becomes container-coordinates, a capture
  snapshot becomes captured-context, and rank becomes urgency-rank. The house
  rule stops at the wire format and the command line, both of which are typed by
  people and read by programs in other languages (§A2.1).

- **D-23** (2026-09-17) — Supervision is a reconciler in the sweep that already
  runs: one retry per tick rather than exponential backoff, since the tick is
  already the rate limit and a bounded attempt count already stops the loop.
  Configuration reload then needs no code of its own. The only state kept is a
  consecutive-failure count per integration, reset once a child has stayed
  connected for more than a minute, so an integration that worked all day and
  then crashed is not mistaken for a crash loop.

- **D-24** (2026-09-17, while building M2) — Four corrections that building the
  paths and the configuration forced, none of them reversals of a decision but
  all of them changes to what §A7.2 and §A14 said.
  - **The socket check is against 103 bytes, not 104.** 104 is the size of
    `sun_path`; the kernel needs the NUL. Measured, both transports.
  - **`AGENT_NOTIFY_ROOT` takes the configuration file too.** D-20 said "state
    and runtime"; an isolated instance reading your real configuration is not
    isolated, and the point of one variable was that isolation costs one step.
  - **The log is `agent-notify.log`.** Two processes write it; naming it after
    one of them is a misnomer that would mislead exactly the person reading it
    to find out which process misbehaved. Each line carries its `component`.
  - **Every tool appears in the configuration exactly once.** Glyphs moved from
    `[display.zellij.glyphs]` into `[integration.zellij].settings`, where R7 says
    core cannot read them anyway, and `[container.tmux]` became
    `[integration.tmux]`. §A14 already stated the rule; its own sketch broke it.

- **D-25** (2026-09-17, while building M3) — Four things the record and the
  reducer settled, two of them corrections to this document.
  - **`ended_reason` is cut.** §A7.4 listed it and §A5.6 had already said the
    reason is a detail. Two fields for one fact is two fields that will
    disagree, and the detail is the one every display already reads.
  - **The three "unchanged" rows of §A5.7 are one rule**, which is how the
    reducer is written: leave a live session where it is, bring a dead or absent
    one back as idle. Resurrection is not a special case of `session-started`;
    `session-started` is a special case of resurrection.
  - **`rank` is restamped only when the kernel actually moves.** A record
    carrying a kernel from a newer core keeps the rank that came with it, rather
    than being handed `RankUnknown` by a build that has never heard of the state.
    This is what makes R21 work in the direction it was written for.
  - **`schema` is never downgraded.** A record read at schema 7 and rewritten by
    this build is written back as 7, because the fields that made it 7 are still
    in there — they were round-tripped, not dropped (R28). Stamping our own
    number over it would be a lie about the content. *[amended 2026-09-22 —
    D-77] There is no `schema` field and no round trip; a field this build does
    not know is dropped on the next write.*

- **D-26** (2026-09-17, after M3) — **One retention duration, not two.**
  `retention-visible` is removed and `retention-known` becomes
  `keep-ended-sessions`. The second duration was presentation policy sitting in
  core (R9), it required a display's contents to change because the clock moved
  rather than because anything happened (R26), and it was in direct conflict
  with the picker, whose entire job is to list the sessions you can still
  resume. What it was trying to buy is bought better elsewhere: a subscriber
  declares whether it wants ended sessions in its opening snapshot (R23), the
  transition to `ended` is delivered to everyone regardless, and a display that
  wants an ended session to linger renders that from `ended_at` in its own
  settings. The default answer to "should an ended session appear on my bar" is
  now **no**, immediately, rather than "for ten minutes".

- **D-27** (2026-09-17, resolving Q15) — **Capture is two functions, and a
  record has three sections.**
  - An integration ships `capture-environment`, run as a child of the hook
    because only a descendant of the agent can read its environment, contracted
    never to talk to its tool; and `interpret-environment`, run in the connected
    daemon where blocking is survivable. A declared list of variable names was
    considered first and is not enough: a capture may read a file or parse a
    socket path, which is arbitrary logic, which is code.
  - This deliberately weakens R13 a little — third-party code now runs on the
    hook path — and the weakening is bounded by a named timeout, by capture
    happening at session start rather than on every hook, and by the half that
    blocks being the half that stays out. The alternative, keeping the hook
    pure, cost a config file full of variable names the user should never have
    had to know.
  - **§A11.5 is reversed**: container-coordinates are no longer "an annotation
    written by a container". They live in `derived_context`, with everything
    else worked out from the same snapshot, because they share its lifetime and
    a task label does not.
  - **R11 is restated**, because a child process inherits an environment and it
    cannot be scrubbed without removing what it exists to read. Core stores what
    an integration returned; an integration you enabled sees where it was
    started. That is the boundary the config file already drew.

- **D-28** (2026-09-17, while building M4) — Four things the store settled.
  - **`fsync`, not `F_FULLFSYNC`.** Go's `File.Sync` is the latter on darwin and
    costs 5.8ms against 160µs, measured. Two writes an event would have been
    12ms on the hook path. What it buys — surviving a power cut — is worth
    nothing to data whose subject is a running process, since the boot id ends
    every session on the way back up.
  - **Destination first, source second**, when a record moves between
    `sessions/` and `ended/`. A crash then leaves two files, and the higher
    `sequence` decides which is real (R16). The alternative ordering leaves
    zero files, and nothing recovers from that.
  - **A writer that cannot get the lock abandons the write.** `lock-timeout`,
    then log and exit 0. Losing an event is survivable because subscribers are
    level-triggered (R22); hanging the agent is not survivable at all (R1).
  - **History is its own directory**, `history/<key>.json`, rather than living
    beside the record. A session that ends moves between two directories, and a
    history that had to move with it would be a second thing to keep consistent
    across the same crash window for no benefit.

- **D-29** (2026-09-17, while building M5) — Process facts, and four things the
  machine does not do the obvious way.
  - **The boot identity on macOS is `kern.bootsessionuuid`, not
    `kern.boottime`.** The latter is derived as *(now − uptime)* and is adjusted
    by sleep and NTP; a boot identity that drifts declares every session dead
    after a nap, which is the worst failure available to this system. Corrected
    in §A8.3.
  - **`EIO` means "no such process"** from `sysctl kern.proc.pid`, and `p_comm`
    carries garbage past its terminator, and a zombie still has a `kinfo_proc`.
    All three are in §A8.2; each would have been silent.
  - **Start times compare to the second.** Exactness would make a record written
    by one probe disagree with the same record read by another.
  - **Three things taken from the Rust implementation**, all logic rather than
    shape: the self-presence rail (a snapshot that cannot see the process taking
    it is not a snapshot of this machine), supersession by identical process
    identity rather than by pid, and the second-resolution comparison. The
    interfaces here owe it nothing.

- **D-30** (2026-09-17, while building M6) — The hook path, and what it taught.
  - **`list` shows liveness but never writes it.** Rendering a killed session
    as working would be a lie; filing it from a polled read command would be a
    write nobody asked for. So the decision is applied to the copy in hand and
    the store is left to the session-watcher (§A7.6).
  - **`list` has a human form and a `--json` form**, chosen by a flag and never
    by whether stdout is a terminal.
  - **Flags are accepted after the argument**, because `focus-session <session>
    --quiet` is how a person writes it and Go's flag package stops at the first
    thing that is not a flag. Nothing there takes a value, so separating
    switches from words is a partition rather than a parse. (The command this
    was written for was `agent-notify name`, which D-60 deleted; the
    accommodation earns its place on what is left.)
  - **`DisplayName`, `State`, `ByUrgency` and `Urgency` are public**, in the
    root package, because R24 says a rule every display would compute
    identically is computed once. The name fallback — the last component of
    `cwd` — is the standing example, and the record stays honest: an empty
    `name` means unnamed, and the guess lives only in the renderer.

- **D-31** (2026-09-17) — **An agent firing a hook at a surprising moment is the
  adapter's problem, not a rule in core.** The reducer's `session-started`
  condition was justified in §A5.7 by Claude firing `SessionStart` on
  compaction. That justification is withdrawn: Claude's hook carries a `source`,
  and an adapter that does not split on it is mistranslating. Compaction is
  `agent-progressed`/`compacting`. The condition itself stays, on
  agent-independent grounds — an existence assertion is idempotent, and the
  branch it uses exists anyway for `context-changed` — but core's reasoning no
  longer leans on any agent's behaviour, which is what R10 is for.

- **D-32** (2026-09-17, while building M7) — **The public surface is two
  packages, and the hook path is one of them.**
  - `agentnotify` holds the vocabulary and the formats and depends on nothing.
    `agentnotify/hook` holds `Record`, the library half of record-agent-event,
    and depends on the internals. M1 said the root package was the entire
    public surface; it cannot be, because the store must import the types and
    the hook path must import the store. Types below, machinery above.
  - **The CLI is now a client of that library rather than a copy of it** (R14).
    `agent-notify report-event` parses arguments and calls `hook.Record`; the
    Go adapters call the same function, so nothing can be true of one and not
    the other.
  - **`agent-notify install <agent>` is a dispatcher and nothing more.** It
    `exec`s `agent-notify-<agent> install` and hands over the terminal. Core
    does not know where Claude keeps its settings or what its hooks are called,
    and R10 says it must not learn.

- **D-33** (2026-09-17, while building M8) — Three things the session-watcher
  settled.
  - **The binary to spawn is not `os.Executable()`**, because the hook library
    runs inside binaries that are not agent-notify. Found by configuration, then
    by name, then by PATH; not finding it degrades to no daemon rather than to
    an error (§A9.2). The bug was found by a live demonstration and the log said
    exactly what was wrong, which is the argument for the log being where R2
    sends everything.
  - **The layout is passed to the session-watcher as arguments**, not left to be
    resolved again. The environment is sanitised at spawn and path resolution
    reads the environment, so a daemon that resolved its own paths could end up
    watching a different directory than its clients write to — and nothing would
    error. This is the hazard M2 found, now closed.
  - **It stands down when its state directory is deleted**, and only then.

- **D-34** (2026-09-18, while building M9) — **The SDK hands a display the
  current state, never a stream of transitions.**
  - `OnChange(View)` receives every session as it now stands. R22 stops being a
    rule an author has to remember and becomes something they cannot break: a
    display written against this is correct after a dropped message, a
    session-watcher restart, a closed laptop, and its own crash, because there
    is no edge to miss.
  - `View.Changed` is the SDK doing the comparison once, for the one subscriber
    that needs it — a notifier, which must not re-announce what it already
    announced. That is R24: the per-subscriber "what I last acted on" belongs
    here and not in every plugin. **[the code did not do this until D-63]**
  - **The fake session-watcher is the real fan-out with a script behind it**,
    not a second implementation of it: the same handshake, the same
    snapshot-on-connect, the same bounded queues and the same overflow. A
    display that works against the fake works against the real one because the
    only difference is where the records came from.
  - **A delta's `event` comes from the poke**, so it is present when a hook
    caused the change and absent when the sweep discovered it. That is the
    honest shape: the field was always an optimisation, never the mechanism
    (§A13.1), and the session-watcher genuinely does not know what caused a
    change it found by reading.
  - **A recording is records with delays, not raw protocol**, and `replay`
    refuses to run without `AGENT_NOTIFY_ROOT` — a recording must never be
    played into the place your real sessions live.
  - **`doctor` connects as an ordinary subscriber**, which is how it tells "pid
    N holds the lock" from "pid N holds the lock and answers" — the wedged
    session-watcher of §A9.2, which it names and never kills.

- **D-35** (2026-09-18, while building M10) — Five things zellij and the first
  real display decided.
  - **A display reads before it writes, every render.** The cheap alternative —
    remember what you last painted — is what the implementation before this one
    did, and it is wrong in three ways a daemon cannot afford: it is incorrect
    after its own restart, it is incorrect after a pane moves to another tab,
    and it cannot tell a rename that would change nothing from one that would.
    One read costs about 15ms and renders only happen when a state moves.
  - **A tab's name is the user's, so it is recovered rather than remembered**:
    whatever the tab says now, minus any glyph this configuration would itself
    have written. That makes a tab renamed by hand keep its new name and makes
    a restart idempotent instead of stacking a second glyph. Stripping is a
    heuristic and it is bounded — it only ever removes a string we would have
    written — which is the price of not keeping a sidecar file of what we
    painted.
  - **A display asks for ended sessions it will never draw.** D-26 says an
    ended session is not displayed; it does not say nothing has to be cleaned
    up after it. A display that painted into a UI it does not own must be told
    when to give the piece back, and the record is the only thing that
    remembers which piece it was. This also fixed the SDK: `WantEnded` now
    means the same thing in a delta as it already meant in a snapshot, so a bar
    that said no never sees one — previously an ended session sat in every
    view until the store pruned it days later, and every display would have had
    to write that filter itself (R24).
  - **zellij's exit code cannot answer "is that session there".** Measured on
    0.45.1: asking for a session that does not exist exits **0** when some other
    detached session happens to be alive — writing the list of sessions that do
    exist where the JSON should be — and exits **1** when none is. A status
    that is a function of unrelated state is not a signal. The parse decides,
    which is a second reason to read before writing: a read that fails means
    that zellij session is skipped and not one rename is attempted against it.
    (A missing *pane* inside a live session does exit 2, reliably.)
  - **A tab wears one glyph, the most urgent, not one per agent.** The previous
    implementation painted a glyph per agent; this is `max(rank)`, which is what
    §A12.1 derived and what makes a tab, an LED over the whole machine and a
    dock badge literally the same function (`MostUrgent`). Count is what a
    picker is for.

- **D-36** (2026-09-18, while building M11) — **A ninth event,
  `turn-interrupted`, reducing to `idle`.** The one thing the second agent
  changed about the design, and the reason M11 was placed where it was.
  - **The evidence.** `Stop` and `Interrupt` are mutually exclusive paths in
    codex: an interrupted turn never reaches the `Stop` call. In the recorded
    traffic an interrupt is followed by silence — once for 32 seconds until the
    next prompt, once for two minutes until the session ended. Mapping it to
    nothing leaves a record saying `working` for all of that, which is the exact
    lie this system exists to prevent.
  - **Why not `turn-finished`.** It promises an answer to go and read, and there
    is none. A notifier acting on the kernel alone would announce "your agent
    finished" a tenth of a second after you pressed Esc — a false statement
    caused by your own keypress. Fixing that by having the notifier check for a
    detail would break R8, which is the rule that a display must be correct
    knowing only the kernel.
  - **Why not `turn-failed`.** `broke` outranks `finished-a-turn` because a
    turn that died without finishing costs you something. An interrupt costs you
    nothing: you are the one who caused it, and you are sitting there.
  - **It also fixed a smell nobody had named.** Before this, `idle` was
    reachable only by `session-started` on a session that was not already live —
    one narrow path to a state whose definition, "alive, nothing pending", is
    much broader. A six-state vocabulary where one state is nearly unreachable
    was evidence the event list was short, not evidence the state was wrong.
  - **It degrades correctly.** A core meeting an adapter that emits an event it
    has never heard of takes the default branch and leaves the session where
    it was — today's behaviour exactly (§A5.7).

- **D-37** (2026-09-18) — **One repository per (tool, role). zellij's display
  and zellij's container are two programs, installed at will.** *Supersedes*
  "one repo per tool" (D-1) and the line in §A10.3 that made zellij the example
  of an integration declaring two roles at once.
  - **They have different blast radii, and that is the whole argument.** A
    display writes titles; a container is the only kind of integration that acts
    on the world rather than describing it — it *moves your focus*. Shipping
    them as one binary means that wanting glyphs on your pane titles enables the
    code that can take your cursor somewhere. Those are different decisions and
    a person is entitled to make them separately.
  - **They fail differently and are supervised separately.** In one binary a
    crash in the container half takes your titles with it, and M13's
    give-up-after-N-failures would retire both. As two children, one can be
    retired while the other keeps painting (R13, which until now had nothing
    inside a single integration to protect).
  - **Each captures for itself, and that is what makes "at will" true.** Both
    want `ZELLIJ_SESSION_NAME` and `ZELLIJ_PANE_ID`, and both ask for them. The
    hook stores the same blob twice, under `by.zellij-display` and
    `by.zellij-container` — about eighty bytes, and the price of neither
    program depending on the other being installed. The alternative, one
    capturing and the other reading its section, would make the display depend
    on a container the user chose not to install, which is exactly the coupling
    this decision removes.
  - **The duplication is smaller than it looks.** The two share the binary
    lookup and `list-panes`; everything else is disjoint — `rename-pane` and
    `rename-tab-by-id` on one side, `go-to-tab-by-id` and `focus-pane-id` on the
    other. A shared library would be a third repository both must version
    against, which is a worse trade for two programs of this size.
  - **It generalises, which is the test of a rule.** aerospace is a container
    and nothing else; sketchybar is a display and nothing else; tmux is a
    container that is only configuration. "One repo per (tool, role)" decides
    all of them without anybody having to argue, and a tool that genuinely is
    one thing is still exactly one repository.
  - **What it costs**: the name in the handshake and in the config table is now
    `zellij-display` rather than `zellij`, and `[container] order` names
    integrations by that same name. Nothing in core changed.

- **D-38** (2026-09-18, while building M12) — **A container is a program with
  subcommands, not a daemon.** *Amends* D-27, which put `interpret-environment`
  on the socket "because the daemon is already connected and can cache what it
  learned".
  - **The daemon introduces a failure mode that masquerades as a legitimate
    state.** §A11.7 requires telling "no container is configured" apart from "a
    container exists but never placed this session" — they feel identical and
    are not. A container that must be connected to interpret adds a third case,
    "its daemon is not running", which produces the *same* empty
    `derived_context` as the second and is therefore indistinguishable from it.
    Spawning has no such case: it either ran or it failed, now, with a reason.
  - **It makes A11.4 literally true.** That section claims a declarative
    container is complete and a coded one is not a special case. If coded
    containers were daemons and declarative ones were command templates, the two
    forms would differ in kind and every caller would carry two paths. As
    commands they are one path with two spellings.
  - **What D-27 was actually protecting is untouched.** Its argument was that
    the half that talks to the tool must not run in the hook — `tmux
    display-message` against a dead server must never be on the path your agent
    waits on. Interpretation still runs in the session-watcher, off that path,
    under its own timeout. "Not in the hook" was the requirement; "in a
    connected daemon" was only one way of meeting it.
  - **The caching it gives up is worth nothing here.** Interpretation runs at
    most once per session per capture — at session start, at resume, and when
    the set of integrations changes — not per event.
  - **So the rule across all three roles is: you connect if you need to be told
    things, and you are run if you need to be asked things.** Displays and
    enrichers connect. Containers are run. Nothing has to be both.

- **D-39** (2026-09-18, while building M12) — **Whatever the hook needs is in
  the config file; everything else is declared on connect.** Not a new rule so
  much as the boundary that was always implied, written down because M12 is
  where it bites.
  - The hook is a fresh process with no socket and no time to open one (R1). It
    cannot ask an integration anything. So the two facts it needs — *which
    variables to read*, and *whether this integration answers
    `capture-environment`* — live in `[integration.<name>]`, written there by
    that integration's own `install`.
  - That is not "roles are configured" (§A10.3). It is the same kind of fact as
    `binary`: where a thing is and how to invoke it, which nothing can discover.
    A role is what an integration *does*, and it still declares that itself on
    connect — where anything that connects can be asked.

- **D-40** (2026-09-18, while building M12) — Four things the containers taught,
  each of which was a bug before it was a decision.
  - **A timeout that kills a child does not bound the wait.** `exec` kills the
    process it started, but `Run` does not return until the pipes it handed that
    process are closed — and a grandchild inherits them. A container that shells
    out to something slow is therefore killed on time and waited for anyway.
    Measured: a 200ms timeout that took **30 seconds**. Every spawn in this
    project now sets `WaitDelay`, and the honest statement of the bound is
    "the timeout plus a bounded flush" rather than "the timeout".
  - **Focus must be idempotent, because zellij refuses to do nothing.**
    `focus-pane-id` on a pane that is already focused exits 2 with "Pane
    Terminal(60) is already focused" — so a focus with nothing left to do
    reported failure having succeeded. It now asks whether you are already there
    first, through the same function that answers `focused`. Anything a person
    can click twice has to survive being clicked twice.
  - **A layer with no coordinates is skipped, not failed.** §A11.2 says each step
    must succeed before the next is attempted, and that is right for a step that
    *ran*. Partial placement is the ordinary case — you started zellij inside a
    terminal window no window manager was recording — and treating it as failure
    would mean anybody whose outer layer is unconfigured can never focus
    anything. Only when no layer placed the session at all is there nothing to
    do, and that already had its own name.
  - **The session-watcher retries an interpretation three times, not once.** The
    first failure is usually a race: a session-watcher can easily ask zellij
    about a pane a moment before zellij is listening. Three sweeps is long
    enough for that to settle and short enough that a genuinely broken container
    is not run for ever. `agent-notify watcher reload` clears the count, which
    makes "I fixed it, try again" a thing a person can say — and is why that
    subcommand, named in §A14 since the beginning, now exists.

- **D-41** (2026-09-18, while building M13) — **What gets supervised is derived
  from `[container] order`, not from a new key.**
  - The supervisor has to decide, before anything connects, whether a binary is
    a daemon to start or a program to run. Roles are declared on connect
    (§A10.3), and a declaration that arrives after the decision is no use.
  - The answer was already in the file. A container is named in
    `[container] order` — the one thing about containers that genuinely is
    configured, because nesting is not discoverable — and a container is run
    rather than started (D-38). So: enabled, has a binary, not in the order.
  - Adding `supervise = true` would have been configuring a role, which §A10.3
    exists to prevent; spawning everything and seeing what sticks would have
    meant starting containers five times each before giving up on them.
  - It relies on D-37: a tool that plays two roles ships two programs, so
    nothing is both a supervised daemon and a container.

- **D-42** (2026-09-18, while building M13) — **The kernel is asked who
  connected; the peer is never believed.** The supervisor's health check is
  "the child I started has connected", which is a different claim from
  "something calling itself zellij-display is connected" — a self-started
  subscriber sharing a name would otherwise make a hung child look healthy for
  ever. A pid in the handshake would have been one field and one lie away from
  wrong, so the pid comes from `LOCAL_PEERPID` on the socket instead (R25). Where
  a platform cannot answer, the match falls back to the name and says so.

- **D-43** (2026-09-18, while building M14) — **A supervised child's environment
  must carry everything that decides where things are, and who you are.**
  *Amends* the "explicit short list" of §A9.2, which was HOME, PATH and
  `AGENT_NOTIFY_ROOT`.
  - **The bug that found it** was silent and total: agent-notify-sketchybar
    started, connected, handshook, was reported healthy by the supervisor, and
    painted nothing at all, for ever. `sketchybar` needs `USER` set to find its
    per-user service and exits with "'env USER' not set! abort" when it is
    missing.
  - **The worse one it exposed** had never been hit because every demonstration
    set `AGENT_NOTIFY_ROOT`. Without it, `paths.FromEnvironment` computes the runtime
    directory from `TMPDIR` — so a child with no TMPDIR looks for the socket in
    `/tmp/agent-notify-<uid>` while the session-watcher listens in
    `/var/folders/…`. In a real install the child would never connect, be killed
    after the grace period, and be given up on with a message about handshakes.
    TMPDIR and the XDG variables are not preferences; they are part of the
    answer to "where is the socket".
  - **So the list has two kinds of thing on it and nothing else**: what decides
    where things are (`TMPDIR`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`,
    `XDG_RUNTIME_DIR`, `XDG_DATA_HOME`, `HOME`, `PATH`, `AGENT_NOTIFY_ROOT`) and
    who you are (`USER`, `LOGNAME`). Nothing on it can hold a secret: a key
    lives in an agent's own environment and is named after the service it opens,
    never after a directory or a person. §A15's promise is intact.
  - **And a display must not be able to fail in silence.** sketchybar's exit
    code reports only the last message in a batch, so it cannot be a verdict on
    a render — but a non-zero exit with nothing marked `[!]` is sketchybar
    refusing wholesale rather than objecting to a message, and that is now an
    error rather than a shrug. The general rule: when a tool's per-item errors
    are not fatal, the case that *is* fatal has to be named, or the display
    reports perfect health while drawing nothing.

- **D-44** (2026-09-18, while building M15) — **A window is not a property of a
  session, so this system stores nothing about one.** *Amends* §A11.1 and
  §A11.4, which both assume that interpretation is where a container does its
  lookup.
  - **Every other coordinate is a property of the session.** A pane belongs to
    its zellij session for as long as both exist, which is what makes "interpret
    once, keep until the capture is replaced" correct for it. A window is not: it
    merely *shows* a session, right now, and stops the moment somebody detaches.
    Attach the same session from a different terminal and the window is a
    different window, with the agent's process unchanged and nothing to
    re-trigger a capture.
  - **So a stored window id is a cached guess, not a coordinate**, and §A11.3
    already forbids trusting those. aerospace's `interpret-environment` is
    therefore pure — it shapes keys and asks aerospace nothing — and the lookup
    happens inside `focus` and `focused`, where the answer is used in the
    millisecond it is obtained. A listing costs about 12ms, which is what makes
    asking every time affordable rather than merely correct.
  - **There is a second reason, and it is about staleness of a nastier kind.**
    Interpretation runs once per capture and its answer is kept; an interpret
    that asked aerospace would be asking at whatever moment the session-watcher
    happened to sweep. A container that was not running then would store an
    empty answer for ever, and one that was would store an answer that is only
    true until somebody moves a window.
  - **What is stored instead is two keys**: the process chain, which names an
    application and is exact when that application owns exactly one window, and
    a window-title prefix built from a template, which is the only index that
    survives a multiplexer. Neither can take you to the wrong place on its own
    — they are looked up, and the lookup either finds one window or says which
    of the named failures it is.
  - **The generalisation**: interpretation is for what is durable about a place.
    Where a place is only true "right now", the honest interpretation is the key
    you would search with, and the search belongs at the moment of use.

- **D-45** (2026-09-18, while building M15) — **"This layer has nothing to do
  for this session" has two spellings, and the focus walk steps past both.**
  *Amends* the walk of §A11.2, which skipped a layer only when the record had no
  section for it.
  - **The bug is a regression caused by installing something.** A container that
    has coordinates and answers `never-placed` used to stop the walk, so adding
    an outer container broke focusing for every session that outer container
    could not see — the inner pane, which was right there and would have worked,
    never got asked. Installing a window manager integration must not be able to
    take away the navigation you already had.
  - **Interpretation is not a yes-or-no**, which is what makes the second
    spelling necessary. aerospace interprets every session it is handed and can
    place only the ones it can see a window for; the ones it cannot are not
    failures and not errors, they are sessions it has nothing to say about.
  - **What still stops the walk is everything that means "I know where this is
    and could not get you there"**: `place-is-gone`, `ambiguous`, `not-running`,
    `refused`. The line is between a layer with nothing to do and a layer that
    tried.
  - **`ambiguous` is new, and it is the other half of the same rule.** A
    container that can see several places the session could be must not pick
    one: arriving at the wrong window looks exactly like success, which §A11.3
    calls worse than not arriving. It is a word rather than a sentence because
    the person can act on it — name your multiplexer session, or keep one window
    per terminal.
  - **A declarative container's unfilled placeholder is NOT this**, and that
    took a failing test to notice. A template is only ever reached for a session
    the container did capture, so a placeholder nothing fills is the capture list
    and the focus command disagreeing: two lines of one table, written by one
    person, and a mistake they want to be told about rather than walked past. It
    is a refusal.

- **D-46** (2026-09-18, switching a real machine over to it) — **A reload
  restarts every supervised integration.** *Amends* §A14's reload, which re-read
  the file and left the children running.
  - **An integration reads its settings once, at startup**, out of the very file
    core has just re-read (§A10.4). A reload that updates only core's copy
    therefore changes core's mind and nothing else, and the person who has just
    edited their glyphs sees nothing happen and concludes the setting does not
    work.
  - **Found by doing it**: a rebuilt display kept running the old binary through
    a reload. Only `watcher restart` picked it up — and that restarts the daemon
    too, which is not what somebody asking for a reload is asking for.
  - **It reconciles immediately** rather than waiting for the next sweep,
    because a display takes its items off the bar when it stops and the gap
    should be a blink rather than a sweep interval.
  - **It does not try to work out who was affected.** Comparing settings tables
    to decide which children need restarting is a cache, and it would be wrong
    the first time somebody changes something no table mentions — the binary at
    the path, for instance.

- **D-47** (2026-09-18, a day of using it) — **A display may announce, and an
  announcement is a pure function of the record and the clock.** *Extends* M14's
  bar, which counted and waited to be looked at.
  - **The problem a counter cannot solve.** A bar is read when it changes, and
    nothing about a number going from 1 to 2 changes enough to be seen. So a
    state that has just arrived is drawn differently: its counter lit, its chip
    opened on the agent's own words, its row lit inside it. Eight seconds, and
    then all of it back.
  - **The deadline belongs to the record, not to the display.** It is
    `state_since + window`, so any paint computes the same answer and no paint
    can extend it. The alternative — `now + window`, remembered — keeps one
    announcement lit for as long as anything else on the machine is happening,
    because every other agent's event repaints the whole bar. This is the same
    rule as R26's elapsed time: a fact every paint derives identically is not
    state, and storing it is how it goes wrong.
  - **`working` does not announce.** An agent getting on with it is the state
    the bar is in most of the day; a bar lit for it would be lit all day and
    mean nothing. The rule is "more urgent than working", written in ranks
    rather than as a list of states, so an agent-integration adding one does not
    have to come back here.
  - **Opening and closing a chip are the only EDGES in a level-triggered
    display** (R22), and they have to be: `popup.drawing` set on every paint
    would shut a chip somebody had opened with their pointer, one agent event
    later. So the display remembers exactly one thing — what it announced last
    — and touches a popup only where the answer changed.
  - **A chip somebody is reading is never taken away.** The hover scripts write
    a mark; the display reads it at the moment an announcement ends and lets the
    chip stand if a pointer has been there since. What closes it then is the
    pointer leaving the bar, like every other chip.
  - **A clock that is already asleep is a deadline nothing reaches.** Found live:
    the announcement raised and stayed lit for the rest of the refresh interval,
    because the repaint timer had begun its thirty seconds before the
    announcement existed. The paint that raises one now re-arms the clock.

- **D-48** (2026-09-18, building the preview) — **What is WRITTEN to a display
  and what is DRAWN on it are different questions**, and a scroll is the answer
  to the second one only.
  - **The preview of an agent's last message is a fixed pool of lines.** Every
    line the agent wrote is written; only the first screenful is drawn.
    Scrolling is `drawing=on` and `drawing=off` over slots that already say the
    right thing, so a wheel never moves text and nothing it does has to be
    quoted. Writing only the visible lines is what makes a scroll reveal blanks.
  - **A hover carries its own answer.** The text is baked into the row's script
    by the paint that wrote the row, so a hover costs one `sh` and one client —
    no store read, and nothing that can be stale, because the words and the row
    were written by the same paint.
  - **That is the one place an agent's words meet a shell** (§A15), and it is
    quoted for it: inside single quotes `sh` expands nothing — measured against
    the real daemon, `$(...)`, backticks, `$HOME` and `;` all arrive as
    characters — so the only dangerous character is the apostrophe, which
    becomes a typographic one, and the only breaking ones are the controls. It
    is applied where the quote is written and nowhere else: a label is argv and
    never sees a shell.
  - **The structure is now sent separately from the values.** A preview forty
    lines deep behind five counters is two hundred items, and re-declaring all
    of them on every paint is a thousand arguments per agent event — measured by
    a bar that stopped answering a `--query` underneath it. The structure is
    re-sent when the bar says it has lost something, which is the same
    self-healing the single-message version had, at a fraction of the traffic.
  - **Two more measured facts about sketchybar**, both of which cost time: a
    backslash in any property makes that item's own `--query` answer invalid
    JSON, and an item's script also runs on a forced `--update`, so every script
    must guard on `$SENDER` or a bar reload opens a chip nobody hovered.

- **D-49** (2026-09-18, building M15a) — **The boundary between a display and a
  UI toolkit is a document, not an API.**
  - The whole of what AppKit is told is a `Paint`: a title in coloured spans and
    a flat list of menu rows, marshalled to JSON and handed across as one
    string. `menubar.m` applies it and decides nothing — down to the gap between
    two counts, which is written into the span that follows one rather than
    inserted by whatever draws it.
  - What that buys is what this project has bought everywhere else: the display
    is a pure function and its tests run on a machine with no screen. Of this
    integration's forty-five tests, four need a menu bar.
  - The alternative is a cgo surface of "set the title", "add a row", "colour
    that one" — a second program, in a language this project has no tests in,
    which is what a UI integration usually turns into.
  - It is the same seam M14 has, where `Render` returns sketchybar's own argv,
    and that is the point: two displays over two utterly unlike toolkits split
    in the same place.

- **D-50** (2026-09-18, building M15a) — **A display may announce; it may never
  take the keyboard.**
  - M14's announcement opens a chip, and a chip is a drawing. The native
    equivalent would be opening the menu — and an NSMenu opening starts a modal
    tracking loop that holds the keyboard until it is dismissed. A display doing
    that on its own initiative would eat what somebody was typing, several times
    an hour, for a notification they did not ask for.
  - So the announcement is the item itself: the state's glyph and the session's
    name, in that state's colour, for the same window and on the same
    record-owned deadline as D-47.
  - **There are therefore no edges in this display at all.** D-47 had to remember
    what it announced because opening and closing a chip are edges; the only
    reason to remember here is knowing when the clock must next be woken. The
    title is a pure function of the records and the time, which is what R22 was
    supposed to make possible and what a toolkit that owns its own widgets
    finally allows.

- **D-51** (2026-09-18, building M15a) — **What a paint costs decides what it may
  wake for.**
  - The sketchybar display deliberately leaves `message` off its `WakeOn` list:
    a paint there is a process, and an agent's message changes fifty times in a
    turn. This display puts it on, because a paint here is a JSON encode and a
    dispatch onto a queue.
  - The same field, the same records, opposite answers, and both right. It is
    the first place where two displays of one system subscribe differently for a
    reason that is about the tool rather than about what is being shown.
  - What it buys is that a row's tooltip holds what the agent is saying now
    rather than what it was saying when the state last changed.

- **D-52** (2026-09-18, a day of not being able to see M15a) — **A macOS display
  has to live in a `.app` bundle, and the bundle can be a symlink.**
  - **A bundle-less process has no preferences domain that survives it.**
    Measured: a value written into the `agent-notify-macos-display` domain read
    back correctly and had vanished by the next launch — `defaults delete` then
    reported "Domain not found". So `NSStatusItem.autosaveName` has nowhere to
    file the item's position, and every restart puts the item back in the
    LEFTMOST slot of the menu bar.
  - **On a full menu bar the leftmost slot is not drawn.** Twenty-one status
    items ran contiguously from x=520 to x=1506 on a 1512-point notched
    MacBook, and only those from x≈912 rightwards were on the screen. Ten
    others — Teams, OneDrive, Bitwarden, WireGuard among them — were allocated
    positions and never rendered. The display was correct, the accessibility
    API agreed it was there, and it could not be seen.
  - **The bundle is a wrapper, not a copy.** `Contents/MacOS/<program>` is a
    symlink to the real binary, and exec'ing through it still yields the
    bundle's identity and the bundle's preferences domain (measured). So
    rebuilding the binary changes what the bundle runs, with nothing to
    reinstall and no second copy to go stale. `LSUIElement` keeps it out of the
    Dock; LaunchServices is never involved, because the session-watcher execs
    the path inside it exactly as it execs every other integration.
  - **The M15a test oracle has a limit, and this is it.** `AXExtrasMenuBar`
    reports a position and a size for an item the menu bar has decided not to
    draw, so those four tests pass with nothing on the screen. Knowing an item
    exists is not knowing it is visible, and the only witness to the second is
    a screenshot.
  - **What it cost to find** is the part worth keeping: two confident wrong
    answers first — the colour (it resolves to white at 55% in DarkAqua, and
    was fine) and Hidden Bar (it was pushing the item to x=-1267, and with
    Hidden Bar quit the item was still not drawn). Both were consistent with
    every measurement taken up to that point. What settled it was looking at
    the screen instead of asking the API.

- **D-53** (2026-09-18, integrating with macOS's own notifications) — **The
  notification belongs to the operating system, and getting it there is three
  facts nobody documents.**
  - **A notification is the one EDGE in a level-triggered system**, and §A12.1
    predicted it: of every display worth imagining, the notifier is the one that
    needs the transition rather than the state. So it is the one place that
    remembers anything, and what it remembers is one timestamp per session — the
    `state_since` it last spoke about. The comparison is against the record's own
    clock rather than a flag, for D-47's reason: `state_since` moving IS the
    transition, and a fact every observer derives identically is not state.
    The first view after starting is never announced, or a restart opens with a
    banner for everything that was already true.
  - **The three facts, each measured after the obvious theory failed**
    [2026-09-18, macOS 26.6.2]:
    - A bundle-less process does not fail to notify, it TERMINATES:
      `UNUserNotificationCenter` raises `bundleProxyForCurrentProcess is nil`
      inside a `dispatch_once`. This is the second thing the bundle of D-52 is
      for.
    - An **ad-hoc signature is refused**: "Notifications are not allowed for
      this application", no prompt, status `denied` without anybody being asked,
      and the app absent from System Settings — so there is nowhere to go and
      turn it on. A self-signed certificate in the login keychain, UNTRUSTED,
      works. macOS wants an identity, not a trusted one and not Apple's.
    - **`UNAuthorizationOptionProvisional` is required as well.** Asking for
      Alert alone is refused even when signed properly. With Provisional it is
      granted immediately and without a prompt, notifications arrive quietly in
      Notification Centre, and macOS then asks "Keep receiving notifications
      from this app?" — which is where a person promotes them to banners. Quiet
      first is also the right default for something nobody asked to be
      interrupted by.
  - **A decision macOS makes about a bundle identifier cannot be unmade.** An
    identifier that was ever used with an ad-hoc signature keeps its `denied`
    after being signed correctly, and there is no reset. Two identifiers were
    burned finding this out. Choose it before the first run.
  - **What it replaces.** D-50's announcement — a few seconds of the session's
    name, added after the counts — is off by default now. It was this display's
    own idea of a notification and a worse one on every axis: invisible unless
    you happen to be looking at the menu bar, and it changes the item's width on
    a bar where width is the scarcest thing there is. What it may no longer do
    is take the item over: the counts are the item, and an announcement is
    added to them or it does not happen. The operating system has a
    notification system, it is configurable in System Settings, and this is not
    the place to reimplement it.
  - **The lesson that keeps recurring in M15a**: every wrong answer here was
    confidently derived from a real measurement — the colour resolved correctly,
    the accessibility API really did see the item, the bundle really did fix the
    crash. What settled each one was looking at the screen.

- **D-54** (2026-09-18, splitting M15a in two) — **Notifications are their own
  integration, not a setting on a display.** `agent-notify-macos-bar` draws the
  semaphore; `agent-notify-macos-notifications` interrupts you. Two
  repositories, two bundles, two identifiers, started separately by the
  session-watcher, neither aware the other exists.
  - **The reason is D-53's own finding, taken seriously.** macOS files its
    decision about notifications against a BUNDLE IDENTIFIER, and that decision
    cannot be unmade. An identifier is therefore not an implementation detail of
    a program, it is a permanent public commitment, and the program that asks a
    person for permission should be the one that needs it — with nothing else
    riding on the same identifier, so that a change to the bar cannot cost
    somebody their notifications.
  - **They are also genuinely separable**, which is the test D-37 asks of any
    split: somebody may want banners and no item on their bar, or the item and
    no interruptions. Neither is a degraded version of the other.
  - **This is not an exception to D-37, it is a reading of it.** Both are
    displays, so "one repository per (tool, role)" looks violated until the
    tool is named properly: the menu bar and Notification Centre are two
    surfaces that happen to belong to one operating system, as unrelated to each
    other as sketchybar is to zellij. The rule counts SURFACES, not vendors. The
    evidence that they really are two tools is that they share no API at all —
    `NSStatusItem` and `UNUserNotificationCenter` have nothing in common but
    Cocoa — and that each has its own failure mode, its own permission, and its
    own way of being invisible when it is broken.
  - **Two displays, two config tables, and each refuses the other's keys.**
    `[integration.macos-bar]` and `[integration.macos-notifications]`, both with
    `role = display` in the handshake, both reading only their own table out of
    the one file everybody shares — which is the ordinary arrangement in this
    system and not a special case (§A10.4). There is a test in each repository
    that the other's settings are neither read nor a reason to refuse the file.
  - **What is duplicated, and why that is the cheaper coupling**: the bundle
    writer, the icon drawing, the sprite, and the four-line predicate for "is
    this worth interrupting somebody about". Roughly four hundred lines. The
    alternative is a shared module between two integrations, which is a
    dependency to version, release and keep compatible — for code that changes
    about once a milestone. Nine lines of ASCII sprite copied into a second file
    is not the kind of duplication that hurts; a third repository between two
    programs that never talk to each other is.
  - **The predicate is deliberately written twice and must agree.** Both say
    "rank above working", both are written in core's vocabulary rather than as a
    list of states, and that is what keeps the flicker on the bar and the banner
    on the screen talking about the same thing.
  - **The banner wears the same mark as the bar, in the same colour**, and that
    took the only per-notification picture macOS has: an ATTACHMENT. The app
    icon is fixed for the whole app — it is drawn in Iris exactly so that it
    never reads as a state — and an attached PNG is what can be the colour of
    what just happened. macOS MOVES an attachment into its own store when the
    request is added, so the file handed over is gone afterwards; the cache that
    draws them is therefore built around redrawing whatever has been taken away,
    which is one `stat` per notification and at most one file per colour.
  - **A program that posts is not therefore a program that can be talked to**
    [measured 2026-09-18, macOS 26.6.2, and the worst bug in this milestone].
    The notifier never called `[NSApplication sharedApplication]`, so `NSApp`
    was nil, `[NSApp run]` was a message to nil that returned at once, and the
    main queue was never drained. Every check passed: permission granted,
    banners delivered, `check` green — because posting is XPC and needs no run
    loop. Only the way BACK was dead, and macOS's answer to a tap on such a
    banner is *"the application is not responding properly"* while LaunchServices
    launches a SECOND copy of the app to look for somebody at home. The general
    form worth keeping: **an integration that only ever pushes can be completely
    dead in the direction it does not push, and none of its own tests will
    notice.** The question that catches it is one block on the main queue,
    asked from another thread, and it is now a test, a `check` line, and a
    warning the program raises about itself five seconds after starting.
  - **Found by the split**: `NotifierPost` reached
    `UNUserNotificationCenter` with no bundle guard, which terminates the
    process. It had been unreachable in one program because the only caller ran
    after `Start` succeeded; on its own, with a test that posts from the bare
    test binary, it aborted the suite immediately. **Separating two things is a
    way of testing each one's edges**, and this is the edge that kills.

- **D-55** (2026-09-18) — **The command line is cobra, and the reason is
  completion that knows what is running.**
  - **What it costs**: two dependencies where there were none in `cmd/`
    (`spf13/cobra`, `spf13/pflag`, plus `inconshreveable/mousetrap`), in a
    program that was deliberately built on the standard library. Core the
    LIBRARY is untouched and still depends on `go-toml` and `x/sys` alone —
    which is the line that matters, because an integration links the library
    and not the command.
  - **What it buys is not `--help`.** It is that `focus-session <TAB>` offers
    the sessions that are running, most urgent first, each glossed with what it
    is doing; `install <TAB>` offers the integrations that are on PATH, which is
    the same question `install` itself asks a moment later; `--wake-on <TAB>`
    offers the record fields. None of that is a static completion script. It is
    the program answering its own question at the moment somebody asks it, and
    hand-writing it would be a script per shell, each with its own copy of the
    protocol, each able to go stale.
  - **What it deleted**: a hand-kept usage string that had to be edited whenever
    a flag changed, eight `FlagSet` blocks, a `partition` helper that existed
    only so that a flag written after a positional argument would still be a
    flag, and the `switch` that dispatched fourteen commands.
  - **The honest accounting on size**: +380 lines and −281 across the command
    files, plus 145 new in `root.go`. It is not smaller. Nearly all of the
    growth is PROSE — every command now carries its own description and its own
    flag help, where before there was one terse block documenting all of them at
    one level of detail. The mechanism shrank; the documentation grew, and it
    grew where somebody will actually read it.
  - **What could not change**: `report-event` still exits 0 whatever happens and
    still says nothing on stdout (R2) — cobra is told to tolerate unknown flags
    there and to keep its usage and its errors to itself, and the binary-level
    test that asserts all of it is unchanged and still passes. `focused` still
    answers with three exit codes rather than an error. `install` still parses
    none of the options after the integration's name, because they are not ours.
  - **The suite made this safe**: every test of the command line drives the real
    binary, so the whole conversion was checked against the same contract as
    before rather than against a rewritten set of expectations. That is the
    argument for end-to-end tests at a boundary somebody might one day rewrite.
  - **The simplicity pass afterwards**, same day, found what a conversion leaves
    behind. A hand-rolled comma splitter — twenty lines reimplementing
    `strings.Split` — went when `--wake-on` became a real slice flag, which is
    also what makes repeating the flag work. `install` was answering `--help`
    with a two-line usage of its own, because `DisableFlagParsing` means cobra
    never sees that flag; it hands the question back to cobra's help now, so the
    description exists once. `main` was reading `os.Args[1]` to recognise
    `report-event`; `ExecuteC` hands back the command that failed, so it asks
    cobra instead of guessing. `--wake-on` no longer completes to `sequence`: it
    is a stamp, waking on it is waking always, and a test now holds the offered
    list to fields a record actually has. And three doc comments were repeating,
    nearly verbatim, what the command's `Long` already says to the user — which
    is the one real hazard in the growth accounted for above, because a rule
    written in two places is a rule that will be edited in one.

- **D-56** (2026-09-18) — **A name says what the thing is, and the language's
  idiom loses when the two disagree.** Two rules, applied to every module,
  file, type, function, field and variable: no name by allusion (`store`,
  `paint`, `marks`, `Singleton`, `Judge`), and no bare noun or verb that leaves
  "of what?" unanswered (`View`, `Facts`, `Take`, `Validate`, `Apply`). A name
  is allowed to be long — `MostAncestorsWorthClimbing` and
  `TheVariableThatNamesTheRoot` are correct, not verbose — but length earns
  nothing by itself: `TheVariableThatMovesEverything` was rejected for naming a
  side effect instead of what it holds.
  - **Applied in three passes on one day**, and the passes are the evidence for
    why the rule is checked while writing rather than afterwards: the command
    line and the two displays first, then the library. `gopls rename` does the
    work, renames every reference including tests, and leaves prose alone —
    except the renamed symbol's own doc comment, where it substitutes by word
    match and produced `a record has more completeTheRecordFieldsWorthWakingFor
    than these`. Every doc comment a rename touches has to be read afterwards,
    and about half want rewriting anyway: a function called
    `howLongItHasBeenInThisState` no longer needs a comment saying it is how
    long the session has been where it is.
  - **The house style was already in the codebase** — `MostAncestorsWorthClimbing`,
    `flushAfterKilling`, `MaxMessageBytes`, `SaidSomething`, `WhoHolds` — and
    clustered in whatever was written last. The library was not badly named so
    much as inconsistent with itself, and the older the file, the more Go idiom
    survived in it.
  - **Open**: the public API is still in the old vocabulary — `subscribe.View`,
    `subscribe.Fake`, `agentnotify.Apply`, `Reduce`, `Differs`, `Ago`,
    `Record.Elapsed`, `Palette.Marks`, `container.Main` and the three-valued
    `Answer`/`Verdict` pair that exists twice under two packages. Renaming those
    touches all nine repositories at once, which is why it is its own sweep
    rather than part of this one. A half-renamed API is worse than either end
    state, because an integration author meets both vocabularies.

- **D-57** (2026-09-19, auditing what the config file exposes) — **A container
  is always a program, and the config file holds nothing an integration knows.**
  *Reverses* §A11.4, which said most containers need no code. *Amends* §A10.3,
  §A14 and §A15.
  - **The audit found one silent failure and it was structural.**
    `agent-notify-zellij-display` holds `ZELLIJ_SESSION_NAME` and
    `ZELLIJ_PANE_ID` in its own source twice — the consts its capture reads and
    its `PlaceOf` reads back — and then required a *third* copy, as
    `capture = [...]`, in the user's config. Edit that third copy and nothing
    errors: the hook captures the wrong variable or none, `PlaceOf` finds
    nothing, and the display paints nothing for ever. A file we invite people to
    edit held a value only the program could get right.
  - **The rule this yields is the one the file should always have had.** A key
    belongs in the config only if the user is the one who knows the answer. Their
    agents, their tools, their retention, their glyphs — yes. Which environment
    variable a program reads — never.
  - **Its sibling had it right all along**, which is what makes the diagnosis
    certain rather than aesthetic. `agent-notify-zellij-container` names the same
    two variables in the same two places and reads them *itself*, in
    `capture-environment`. Nothing in its table can be typed wrong, because
    nothing in its table is a variable name.
  - **The declarative container is what kept `capture` alive**, so it goes. It
    had no users — both real containers ship a binary — and it was structurally
    incomplete: it can never answer `focused` (§A11.6), and one argv with no
    conditionals cannot express a correct tmux focus anyway. Removing it takes
    `runTemplate`, `substitute`, three `HasABinary` branches, the env-reading
    branch on the hook path, and two config keys with it.
  - **`capture-environment` survives the same test, narrowly.** The hook has no
    socket and cannot ask (D-39), so *whether to run this binary* must be read
    from somewhere. One boolean written by an `install` is the smallest thing
    that can carry it, and its wrong value fails coarsely — the integration
    captures nothing at all — rather than by painting nothing.
  - **`Hello.Environment` was the declared mechanism and was never wired up.**
    The SDK sent it and core read it nowhere; it could not work, for exactly the
    D-39 reason — capture happens in the hook, and this arrives over a socket
    the hook does not have, long after the only process that could have acted on
    it has exited. It is removed rather than left looking like the answer.
  - **A10.5's additive-only rule does not bind yet, and this is the first place
    that mattered.** It is a promise to repositories we do not control, and
    there are none: the module is unpublished, `Version` is `0.0.0-dev`, and
    every integration reaches core through a `replace` directive to a sibling
    directory. Honouring it now would mean carrying dead surface into 1.0 to
    keep faith with nobody. The rule starts applying the day the module is
    published, and until then the whole consumer set is the nine directories
    here — which is also the whole test.
  - **The cost is one process spawn per hook per capturing integration**, which
    is the trade being accepted deliberately. It is bounded by `capture-timeout`
    and the captures run concurrently, so the worst case is one timeout however
    many are installed. See D-58, which is the half of that cost nobody was
    getting anything for.
  - **A removed key gets a sentence, not a shrug.** A config still carrying
    `capture` or `focus` would otherwise get the generic "key not recognised",
    which says something was ignored but not that the mechanism has gone. It gets
    a named complaint telling the person to delete the line and re-run the
    install, and `config.removedKeys` can be deleted when no such file is left.

- **D-58** (2026-09-19, measuring what D-57 costs) — **Decide whether to
  capture before capturing, not after.** *Amends* §A10.3's "capture is not on
  every hook", which described the intended behaviour correctly and did not
  match the code.

  - **Every hook ran every capture and then threw the answer away.** `look` called
    `capture()` unconditionally and asked `worthCapturing` afterwards, and the
    answer is almost always no, because an agent's environment does not change
    while it runs. [measured 2026-09-19, macOS 26, three capturing integrations]
    A hook costs 9.3ms when it captures and 4.9ms when it does not — so the
    spawns were **most of what a hook cost**, on the one path R1 governs, and
    every one after the first was discarded. Over the 184 hooks of the session
    this was found in, about 0.8s of an agent's wall-clock for 4.4ms of use.
    An earlier figure of 31ms per hook was wrong: that timing loop was measuring
    the `python3` it used as a stopwatch rather than the spawns, which is a
    reminder that a measurement quoted in this document has to be one somebody
    can reproduce.
  - **It was not carelessness, it was one condition.** Of the three tests in
    `worthCapturing` — nothing stored, the process changed, the set of
    contributing integrations changed — only the third looked at the result, and
    it compared against `captured.By`, the set that *answered*. Wanting to know
    who answered is what made everyone run.
  - **The set is knowable from the configuration**, because it is the same
    predicate `capture()` loops over: enabled, `capture-environment`, has a
    binary. `wouldBeAskedNow` computes it without spawning anything, and the
    steady state now spawns nothing at all.
  - **The predicate is not duplicated, and the test that would have guarded the
    duplication is not written.** The first cut of this had `wouldBeAskedNow`
    beside capture's own loop, a comment on each saying they must agree, and a
    test spawning real programs to check that they did. Then capture's loop
    became `for _, tool := range wouldBeAskedNow(settings)` and all three stopped
    meaning anything: a test asserting that capture runs exactly the list capture
    iterates is a function read back to itself, dressed up in temporary
    directories and shell scripts.
    What is tested instead is the predicate itself — a table with no filesystem
    behind it — plus the one thing capture does beyond running the list, which is
    keeping each blob under its own name and letting a failure contribute
    nothing. That last distinction is load-bearing: a failure that left a blank
    entry behind would read as a success to `worthCapturing`, and that session
    would never capture again.
    **The general form**, since this was nearly shipped: a test that guards a
    duplication is worth exactly as much as the duplication is unavoidable. Ask
    whether the second copy can be deleted before writing the test that watches
    it.
  - **An integration whose capture fails still recaptures every hook**, and that
    is accepted rather than solved. It is what the old code did for everyone
    unconditionally; it now happens only when something is broken, the failure is
    logged each time, and the common near-miss is not a failure at all — an agent
    outside zellij makes `capture-environment` return two empty strings, which
    is an answer.
  - **The deeper fix is left alone on purpose.** `Apply` voids the whole
    `DerivedContext` when any capture arrives, but the watcher's `derive` already
    pairs coordinates to captures per name, so the wholesale nil is coarser than
    §A7.4.1 requires: zellij recapturing discards aerospace's coordinates, which
    then cost an `interpret-timeout` to rebuild. Making both halves per-name
    would let the hook spawn only what is missing and delete the set comparison
    entirely — but it changes what `Report.CapturedContext` means, from the whole
    context to a partial update, and that is an amendment to §A7.4.1 rather than
    a performance fix.

- **D-59** (2026-09-19, sweeping up after D-57) — **Capture is its own package,
  because it belongs to no role.** *Amends* §A10.2's display/container
  dichotomy and moves one function out of the public `container` package.

  - **A display was importing the container SDK.** After D-57 there is one way to
    read an agent's environment, and agent-notify-zellij-display needs it — a
    pane title cannot be painted without knowing which pane. So a display called
    `container.Main`, filled in a `container.Integration`, and read a package
    doc opening with a dichotomy it was itself breaking. The code worked; what
    was wrong is that an author following the docs would not have found it, and
    one who did would have concluded their display was a container.
  - **The question was never a container's.** `interpret-environment`, `focus`
    and `focused` are what make a container a container: they are about where a
    session lives and getting you there. `capture-environment` is about *when*
    and *where* code may run — inside the agent, on the path it waits on, reading
    and nothing else — which is orthogonal to what the integration is for.
  - **One implementation, two doors.** `capture.Main` is the whole of it;
    `container.Main` dispatches its `capture-environment` case straight to it, so
    a container and a display answer the same subcommand through the same code
    and cannot drift. A container author sees no change: four functions, one
    main, and `Capture` still declared beside the other three because a container
    usually needs all four.
  - **The core side follows the same seam.** Running an integration's subcommand
    was a private helper inside `internal/containers`, which meant the hook
    imported a package named for containers to do the one thing that is not about
    them. It is now `internal/subcommand`, with the container half left in
    `internal/containers` where it belongs.
  - **`container.CaptureCommand` is gone**, replaced by `capture.Command`. The
    string is unchanged, so nothing on disk or on the wire moves; only where an
    author reaches for it does.
  - **The hook's own `capture` function became
    `askEveryoneWhoCaptures`**, which the package name collision forced and which
    is the better name anyway: it pairs with `wouldBeAskedNow`, and what it
    returns is every answer rather than one capture.

- **D-60** (2026-09-19) — **The name comes from the agent, on the hooks that
  were firing anyway.** *Replaces* §A7.4.2 and deletes `agent-notify name`.

  - **The habit was re-deriving what was already on disk.** The design asked a
    session to name itself by running a command from its own instructions.
    Both agents already keep a name and generate it from the conversation —
    Claude with the prompt *"Generate a short kebab-case name (2-4 words) that
    captures the main topic of this conversation"*, which is the habit it was
    being taught, and codex with a `thread_name` per thread. An instruction
    teaching an agent to redo what its agent does unprompted is an instruction
    to delete, and a tool call per session with it.
  - **`Report.Name` is an optional field beside `Cwd`**, and behaves exactly
    like one: absent means "leave what is stored alone", no event clears it,
    and it never moves `state_since`. The invariant the old command existed to
    protect survives the command — a name is a label, not a state.
  - **Every hook carries it.** The report is written anyway, so the name costs
    no extra write, no extra lock and no extra sequence. It also fixes what the
    habit could not: a rename halfway through a session used to never reach the
    record, and now it lands on the next tool call.
  - **Where the name lives is the adapter's, and only the adapter's.** Claude's
    is `~/.claude/sessions/<pid>.json` — found by `CLAUDE_PID`, confirmed by
    matching `sessionId` because pids are reused and `/clear` replaces the
    session inside a process without renaming the file, and with Claude's
    `derived` names thrown away because `<cwd-basename>-<counter>` says less
    than the cwd fallback. Codex's is the tail of
    `~/.codex/session_index.jsonl`, the projection of `threads.name` from its
    sqlite, which it writes about four seconds into a thread's first turn.
    Core is handed a string (R10).
  - **The hook payload could not have carried it.** [verified 2026-09-19,
    2.1.236] Claude builds every hook input from the same six fields; only the
    statusLine command is given `session_name`, and that slot belongs to the
    user. `CLAUDE_PID` in a real hook's environment was verified the same way,
    by printing it from inside one. *[amended 2026-10-09 — D-88] It does now,
    on `UserPromptSubmit` and `SessionStart`, as `session_title`, and that is
    read first. The placeholder thrown away above ends in a random byte, not a
    counter.*
  - **Neither agent exposes a live per-session directory the way the other
    does.** Claude's `~/.claude/sessions/<pid>.json` has no codex equivalent:
    `~/.codex/sessions/` holds date-partitioned rollout transcripts with no name
    in them, and `~/.codex/thread-writer-locks/<thread-id>.lock` — the nearest
    thing structurally, one zero-byte file per live thread — carries liveness
    and nothing else. So the two adapters read two different files, which is
    exactly what an adapter is for.
  - **What went with the command**: `process.FindAnyAgent`, which existed only
    to resolve the ambient session, and the naming instruction in the user's
    own `CLAUDE.md`. `report-event` gained `--name`, which is how a shell
    adapter reports one.

- **D-61** (2026-09-20) — **Tokens are a field, and the adapter reports
  responses rather than a total.** *Amends* §A7.4 and *adds* §A7.4.3.

  - **`agent_data` was the wrong home, and the table said so itself.** It listed
    token counts there in the same breath as the model, and the two are not
    alike: a display reads the model when it already knows which agent it is
    looking at, and reads tokens across every agent at once. An opaque namespace
    (R7) can serve the first and can never serve the second, so tokens in
    `agent_data` are tokens no bar can count.
  - **What a closed vocabulary costs here is four counters**: `input`, `output`,
    `cache_read`, `cache_write`, disjoint and summing to the total, with
    `reasoning` named as the subset of `output` it is. Claude and codex both
    fill all five without a word being invented, which is the same test the
    nine events had to pass.
  - **The running total cannot come from the adapter.** It is a one-shot process
    that exits in milliseconds, and the total is in the record it has not
    written yet. Reading the whole transcript to rebuild it is a cost that grows
    with the session forever — 51 MB and 3401 assistant lines on this machine
    already. So the report carries what one tail read saw, and `Apply` adds what
    is after the cursor. Core holds the arithmetic because core holds the
    previous value; the adapter holds the file format because that is the only
    agent-specific part.
  - **Deduplicating by the response id is the feature, not the polish.**
    [verified 2026-09-20, 2.1.236] Claude repeats a response's whole `usage` on
    one transcript line per content block: 1869 responses written as 3401 lines,
    so the obvious implementation overcounts output by 2.11×. A number that is
    twice the truth is worse than no number, and the id that fixes it is also
    the cursor that makes a second read of the same tail free.
  - **It wakes nobody by itself**, or a counter moving on every response would
    have undone R23 for every display at once. The general shape worth keeping:
    a field that changes as often as the agent thinks has to be opted into,
    never broadcast.
  - **And opting in needs two gates, which the first implementation collapsed
    into one.** Making `usage` a stamp made the session-watcher drop a
    usage-only write before any subscriber was asked, so `wake_on: usage`
    delivered nothing at all. The unit tests could not see it: they asked
    `Differs` directly and `Differs` was right, while the gate in front of it
    was the one answering the wrong question. It took installing the binaries
    and watching a real session to find, which is the argument for ending every
    one of these that way.
  - **What was left undone on purpose**: cost in currency, which needs a rate
    table §A14 will not hold; and Claude's subagents, which write their own
    transcripts and need a second cursor each.

  **[amended 2026-09-22 — D-76]** The four counters and the cursor all stayed.
  What left is the pair this decision carried alongside them, `usage.context`
  and `usage.context_limit`: a level rather than a counter, fillable by one of
  the two agents, and drawn by one display for that one agent. The argument
  above is untouched by it — tokens are still a field, the adapter still reports
  responses, and core still does the adding.

- **D-62** (2026-09-20) — **A timeout is not a preference.** *Amends* §A14 and
  §A17 R18.

  - **Six of the seven durations left the file**: `sweep-interval`,
    `lock-timeout`, `capture-timeout`, `interpret-timeout`, `integration-grace`
    and `focus-timeout`, along with every integration's own `timeout` and
    `refresh`. Each is now a named constant beside the code it bounds.
  - **The test R18 was really asking is "does this duration have a name and a
    defined behaviour on expiry", and a constant passes it.** Being in a config
    file was never what made a timeout accountable; the comment beside it was.
    What the file added was the chance to get it wrong: nobody editing a TOML
    file knows better than the code how long `zellij action list-panes` should
    be allowed to take, and a person who sets it wrong debugs a focus that fails
    for a reason the file does not mention.
  - **`keep-ended-sessions` stays, because retention is a preference.** How long
    a dead session remains resumable is a real trade a person makes — disk and
    picker clutter against being able to come back on Monday — and nothing in
    the code can make it for them. `announce`, on the two bars, stays for the
    same reason: how long a display shouts at you is taste.
  - **What is left is written in nanoseconds**, and the custom `Duration` type
    that parsed `"7d"` is gone. [verified 2026-09-20, go1.26.2] `time.Duration`
    implements none of `encoding.TextUnmarshaler`, `encoding.TextMarshaler`,
    `json.Unmarshaler` or `json.Marshaler`: it is an `int64` of nanoseconds with
    a parser attached as a package function rather than a codec. So
    `timeout = "2s"` does not decode into one at all, and `timeout = 2` decodes
    to two nanoseconds without complaint. Every duration in this system had
    therefore grown its own workaround — core a wrapper type with a day unit,
    every integration a `string` field and a hand-written
    `time.ParseDuration` — and the five spellings disagreed: `7d` was valid in
    core's tables and an error in an integration's, in the same file.
  - **The cost accepted, plainly**: `keep-ended-sessions = 604800000000000` is a
    fifteen-digit number where `"7d"` used to be. That is the price of having
    one duration type in the system instead of five, on the one key where it is
    paid — and it is paid once, by whoever edits it, rather than every time
    somebody writes a new integration.
  - **The internal ones a test must still shorten are vars, not consts**:
    `watcher.SweepInterval`, `watcher.IntegrationGrace`,
    `sessionstore.LockPatience`, `hook.captureTimeout`. They are in `internal/`,
    so no user can reach them, and nothing at runtime writes them — a test
    watching a loop go round three times should not wait fifteen seconds to do
    it.

- **D-63** (2026-09-20) — **The SDK was not keeping the one promise it made to
  a notifier.** *Amends* D-34 and §A7.7.

  §A7.7 refused the global event log on the grounds that *"the per-subscriber
  state this needs — what I last announced for this session — belongs in the
  SDK rather than in each plugin (R24)"*, and D-34 named `View.Changed` as
  where it lives. Neither was true in the code, and the only notifier in the
  world had quietly written the state itself.

  - **`Changed` meant "since this socket opened", not "since the last call".**
    The map it was compared against was created inside `attach`, which is one
    connection, so the snapshot every subscriber is handed on reconnect was
    diffed against nothing and reported every session in it as changed. A
    session-watcher restarting is ordinary — `watcher reload`, a crash, a new
    version installed, a laptop opening — so a notifier written to the
    documented contract would have posted a banner per live agent every time
    the daemon came back. The map now outlives the connection.
  - **The first view carries no changes**, which is the rest of that sentence:
    nothing has moved when there was no previous call. The notifier's `seeded`
    flag existed for exactly this, and its comment described the bug without
    naming it — *"a display that started thirty seconds ago would otherwise
    open with a banner for every agent that happens to be blocked"*.
  - **The two ends of one field disagreed about what "changed" means.** The
    session-watcher filters a delta on the subscriber's own `wake_on` before
    sending it; the snapshot diff on this side compared everything but the
    stamps. So a display declaring `wake_on = ["kernel"]` — never woken by a
    new message — found one in `Changed` if it arrived while disconnected. Both
    ends now ask the same question.
  - **The transition was on the wire and was being dropped.** Every delta
    carries `previous_kernel` and `event`; both were decoded and discarded one
    line before they would have been handed over. `Changed` is now `[]Change`,
    which carries the record, what it moved from, and what was reported. A
    snapshot fills the previous kernel from what the subscriber was last shown
    — the same fact, worked out rather than told — and leaves the event empty,
    which is honest: nothing told it what happened.
  - **So `PreviousKernel == Record.Kernel` is the whole test a notifier needs**,
    and it needs no memory of its own to apply it. That is what §A7.7 promised
    when it refused the log.
  - **The fake learned to report an event.** It could only ever play an empty
    one, which made it a fake no notifier could be tested against — the single
    thing it exists to prevent. `Fake.Reported(record, event)` plays one;
    `Fake.Publish(record)` still plays a change with no event, because that is
    a real thing to play: a change the session-watcher OBSERVED rather than was
    told about carries none either (D-12). `record`/`replay` carry the event
    through a recording for the same reason.
  - **Why none of it was noticed**: `View.Changed` had two consumers in this
    repository, `tail` and `record`, and both want the record and nothing else.
    The one consumer the field was designed for did not use it. There was no
    test, because there was nothing to test against — which is the argument for
    the five in `subscribe/changed_test.go` now, each of which was confirmed to
    fail with its own fix backed out.


- **D-64** (2026-09-20) — **What is allowed to interrupt a person is one rule,
  and it lives in core.** *Amends* §A12.1 and D-47.

  - **It was written three times**, in three repositories that never compare
    notes: `macos-bar`, `sketchybar` and `macos-notifications` each carried
    `record.Rank > RankWorking` under a local name, each with its own paragraph
    explaining it. Of everything duplicated across the integrations — finding a
    binary, running one under a timeout, appending a TOML table — this is the
    only copy whose drift is *silent*. A seventh state that deserves attention
    would interrupt on the menu bar and say nothing on the phone, and nothing
    anywhere would report a problem.
  - **`Record.WantsYou()` is that rule**, and it asks the record's own rank
    rather than the kernel's, which is the whole reason a rank rides in every
    record (§A5.5): a state this build cannot name arrives with the number the
    build that wrote it assigned, and is judged by that.
  - **`JustArrived` is the pick**, which both bars also spelled for themselves:
    of everything on screen, the one thing worth interrupting for, most RECENT
    rather than most urgent — somebody looking up at a display that just
    changed is asking "what just happened", and urgency has already had its say
    in the order of everything on it. Ties break on rank, then on the name, and
    the last of those is new: without it two sessions blocking on the same tick
    made a display that announced a different one on every repaint, decided by
    whatever order it happened to iterate in.
  - **`Arrival.Until` carries D-47 across**, which is the subtle half. The
    deadline is state-since plus the window, never "when I noticed": a display
    repaints for every change to every session, and a deadline measured from
    the moment of noticing is pushed forward by every one of them, so an
    announcement would last not as long as it was worth saying but as long as
    anything at all was happening.
  - **What stays with each display**: how long the window is, what a lit chip
    looks like, whether a menu opens, where the highlighted row sits. That is
    presentation and it genuinely differs — sketchybar opens a chip on a row it
    has to locate in its own layout, the menu bar deliberately opens nothing
    because an NSMenu takes the keyboard, and the notifier posts a banner. Only
    the two questions they were all answering identically moved.
  - **It is level-triggered like everything else a display is handed** (R22),
    computed from the records and the clock rather than from a transition — so
    a display that restarts mid-announcement carries on announcing, and one
    that missed the delta entirely still says the right thing. That it composes
    with D-63 without needing it is the point: `View.Changed` says what moved,
    this says what is worth saying about it, and neither is required to use the
    other.
  - **The three displays adopt it separately**, because they are three
    repositories and this one has to be published before they can see it.


- **D-65** (2026-09-20) — **Three more fields on the record, and core learns the
  word "git".** *Adds* §A7.4.4, *amends* §A7.4 and R10.

  - **The test each one had to pass is §A7.4's**: name the display that reads
    it. `model` is what a bar groups by and what a display needs before it can
    price the tokens D-61 stored. `quota` is "how close am I to the wall", which
    nothing in this system could answer at all. `branch` is what tells two
    sessions in one repository apart, and it is the first thing anybody asks of
    a list of agents.
  - **`model` was in `agent_data` and was never actually there.** [verified
    2026-09-20, 2.1.236] Claude's hook payload carries no model — the adapter had
    been writing an absent key since it was added, and no record on this machine
    ever had one. That is the argument against opaque namespaces in miniature: a
    field nothing can read is a field nobody notices is empty.
  - **The two adapters answer it differently, and that is the design working.**
    Codex has it in the payload; Claude has it on the transcript line already
    being read for tokens. Neither costs a read that was not happening.
  - **Quota is account-wide and is stored per-session anyway**, because a record
    is the only thing a display is handed. The alternative is a second store
    with a second lifetime and a second retention rule, for one number.
  - **R10 bends for the branch, once, with the reason written down.** Both
    agents believe they know the branch and both are wrong in ways that matter:
    Claude answers `"HEAD"` for a repository on `main` when the session's
    directory merely contains checkouts, and codex records it once at session
    start and never again. Reading `.git/HEAD` beside `cwd` is one file read, no
    subprocess, always live, and identical for every agent that will ever exist
    — and the alternative is those forty lines written once per adapter forever,
    which is the duplication R24 exists to stop.
  - **Worktrees are the case it had to handle**, not an edge case: a `.git` that
    is a file pointing at `gitdir:` is how two agents work on two branches of one
    repository at once, which is most of why the field is wanted.
  - **What stays out**: approval posture, which needs a third closed vocabulary
    and is a decision of its own; and time-in-each-kernel, which is pure core and
    costs nothing but is adjacent enough to the analysis §A1 refuses that it
    deserves its own argument rather than riding in on this one.

  **[amended 2026-09-22 — D-76]** Two of the three fields, not three. `quota`
  went, and it went by failing the very test this decision set for it: name the
  display that reads it. One did — the picker — and it drew the meter for one
  agent of two, because Claude's allowance is in the statusLine payload and that
  is the user's slot. `model` and `branch` both still pass it on every record.

- **D-66** (2026-09-20) — **An integration states what it needs; it does not
  write it.** *Amends* §A14 and D-57, and *reverses* the second half of every
  tool-integration's `install`.

  - **D-57 drew this line and stopped one step short of it.** It said everything
    in the config file is the user's to write, and gave the test: *does only the
    user know the answer*. That test was applied to the file's CONTENT — it is
    what removed the declarative `capture` list — and never to the ACT of
    writing. Seven integrations went on opening the file and appending to it.
  - **And the thing being written was not configuration, it was consent.**
    §A14 says `enabled` is absent-means-true and *"writing the table is the
    ask"*. So the table is not an inert stanza waiting to be switched on: the
    table IS the switch. An install that appended it had not configured a
    display, it had turned it on, and the session-watcher would start it on the
    next reload. Whether something should be running is the definitive case of a
    thing only the user can know.
  - **The containers made the argument against themselves.** Both wrote
    `[container] order` when no `[container]` table existed, directly beneath a
    comment reading *"it is policy rather than a property of this container:
    which shell is outside which is something only the person running them
    knows (§A11.2)"*. Naming a line as the user's and then writing it anyway is
    the whole of what this reverses. That it rarely collided is an argument
    about likelihood, not about whose line it is.
  - **The agent-integrations are not the same case and do not change.** Writing
    `~/.claude/settings.json` or `~/.codex/config.toml` is the ONLY mechanism by
    which the hook can exist at all — without the entry the adapter is inert and
    invisible, and there is no later moment at which a person enables it.
    Agent-notify's own config file is a record of preferences and has other ways
    to say what it needs. One is necessary, the other was convenience, and
    convenience that silently enables something is what D-57 already refused.
  - **What the integration still supplies, because a person cannot**: the
    absolute path of its own binary, resolved — and for the two macOS displays,
    the path *inside* the bundle they build — plus `capture-environment = true`,
    which survives as the one fact core must read somewhere and the hook cannot
    ask for (D-39). It is stated rather than filed.
  - **Stdout is the table, stderr is everything else.** So somebody who has
    already decided can `agent-notify install sketchybar >> config.toml`, and
    nobody ever gets an apology inside their TOML. The picker makes the point
    sharpest: its keybinding is KDL and belongs in a different file, and it was
    always only ever shown.
  - **The bundles are still built**, and that is the line rather than an
    exception to it. A `.app` is the integration's own artifact, macOS gives it
    no alternative, and it lives in ~/Applications rather than in a file
    somebody wrote by hand.
  - **What it cost, and where that is paid.** Installing and enabling used to be
    one act, so there was nothing to notice; now they are two and nothing said
    the second never happened. `doctor` says it: programs on your PATH that
    appear in neither `[integration]` nor `[agent]`, reported as a `--` rather
    than a failure, because a program you have not asked for is not a fault.
  - **About 1,100 lines went**, across seven repositories: the flag parsing, the
    read-parse-check, the idempotency, the append, the "prepend a newline if the
    file does not end in one", and the TOML dependency that existed in six of
    them for no other purpose. What replaced it is a `table()` and a `Fprint`.
    The core `install` package that was about to be written to hold all of it
    was never needed — the right answer was to delete the behaviour, not to
    centralise it.


- **D-67** (2026-09-20) — **Where a tool lives is answered once, in the shell
  that can answer it.** *Amends* §A11.4 and follows D-66.

  - **Four integrations carried the same list**, character for character:
    `{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"}`, scanned when
    `exec.LookPath` failed to turn up zellij, aerospace or sketchybar.
  - **It existed because the lookup was happening at the wrong moment.** These
    programs are started by the session-watcher or run by the hook, and a
    launchd job's PATH is `/usr/bin:/bin` and nothing else. So the lookup ran in
    the one context that cannot answer it, and the list was an attempt to guess
    what the environment would not say. Homebrew's two prefixes are a guess
    about a package manager; `~/.cargo/bin` — where `cargo install zellij` puts
    the program this was mostly used to find — was not in it.
  - **`install` runs in your shell, where the answer is simply there.** So it
    resolves the tool and prints the path, and what remains at runtime is
    reading a string somebody has seen. This is not a new rule: the integration
    has always written its OWN binary as an absolute path resolved at install,
    for exactly this reason. The tool is the same kind of fact, one field to the
    left, and it was the one left to guesswork.
  - **Required, and absolute.** Keeping PATH as a fallback would have preserved
    the bug that created the list — it works when you run the display by hand
    and fails when the session-watcher starts it — and a relative name is that
    same ambiguity spelled shorter. Three ways to answer one question is what
    was wrong; two is not much better. Absent or relative is a startup error
    naming the key.
  - **The key is named after the tool**, `zellij` / `aerospace` / `sketchybar`,
    not `binary`. Since D-66 the install prints the integration's table and its
    settings together, and two adjacent `binary` lines meaning two different
    programs is a thing somebody would get wrong exactly once.
  - **Core already did the better thing for itself.** `watcher.CoreBinary` tries
    the configured value, then this executable, then PATH — and when all three
    fail it says *"Name it as `agent-notify-binary` in the configuration"*. No
    prefixes, no scan. That is the pattern the four integrations should have
    copied and did not.
  - **What it buys beyond the deletion**: the failure mode inverts, from a
    display that is correct and paints nothing to an error naming the key and
    the path; and M17 no longer has to extend four copies of a macOS-shaped
    list with `~/.local/bin`, `/nix/store` and the rest.
  - **Half of a planned change evaporated rather than moving.** The item this
    came from was "four copies of find-the-binary and four of
    run-under-a-timeout, both of which core should own". Only the second half
    is left to share; the first is gone, the same way D-66 removed the install
    helper that was about to be written.

- **D-68** (2026-09-21) — **Running an external program under a timeout is
  core's, not each integration's.** *Completes* the half of D-67 that was left
  to share, and adds `tool` to the exported surface of §A10.4.

  - **Five copies of the same dozen lines.** Four integrations — both zellij
    halves, aerospace, sketchybar — plus `internal/subcommand.Ask` in core.
    Each built a context with a timeout, set `WaitDelay`, captured two buffers,
    and worked out what the failure meant.
  - **Four of the lines are mistakes waiting to be made, and each is silent.**
    Without `WaitDelay` the timeout is a lie: exec kills the child and then
    waits on pipes a grandchild inherited, measured here as a 200ms timeout
    that took 30 seconds. Checking `err` before `ctx.Err()` reports every
    timeout as `signal: killed`, which names the symptom and buries the cause.
    A missing binary and a refused request arrive as one non-nil error and mean
    opposite things. And whatever the tool printed is somebody else's bytes on
    their way to a log and a status bar.
  - **They had already drifted on all four.** Only sketchybar distinguished a
    binary that is not there from one that said no, so zellij's and aerospace's
    containers reported both as the same `container.Problem`. The three copies
    of `summarise` disagreed about whether to strip escapes at all, about
    whether to cap at 120 bytes or 160, and about whether an empty answer reads
    as `"nothing"` or as nothing. The two that did strip understood CSI and let
    OSC — the sequence that retitles a window — straight through, which is
    §A15 broken in the one place core could not see.
  - **The failure is three types, not three strings.** `DidNotRun`,
    `TookTooLong`, `SaidNo`. A container needs the distinction to choose
    between `NotRunning` and `Refused` (§A11.3), and sketchybar needs it
    because for sketchybar a refusal IS the answer: the exit code reports only
    the last message of a batch, so the complaints on stderr are the truth.
  - **A non-zero exit is an error, not a field.** The alternative — a `Code` on
    the result — reinstates the mistake one level down: a caller who forgets to
    check it has a tool that failed and looks like it worked. Reading a failure
    as data stays possible and becomes an explicit act, which is unwrapping
    `SaidNo`.
  - **A zero timeout is refused rather than defaulted.** Supplying one quietly
    would hide the bug in whichever caller forgot, and an unbounded wait is the
    thing R18 exists to prevent.
  - **`Summarise` is built on `CleanMessage` and `CleanLine`** rather than on a
    fourth escape stripper. Core already owns the careful one — it understands
    OSC, repairs invalid UTF-8, and cuts on a rune boundary — and §A15 says
    core is the single place this text is neutralised. First non-empty line,
    because a tool that failed says why first and then prints something large.
  - **Core uses it too.** `subcommand.Ask` is now the JSON half of the
    subcommand protocol and nothing else, which is what it was always supposed
    to be. It still collapses every failure into one error (D-38): the types
    exist for an integration deciding about its OWN tool, which is a different
    question from what core does about an integration that went quiet.
  - **Two call sites were deliberately left alone.** The picker wires
    `focus-session` straight to the terminal so that a refusal is read in the
    words the container wrote, and buffering it would swallow that. The two
    `.app` bundle builders run `codesign`, which can legitimately block on a
    keychain prompt waiting for a person to click Allow — a timeout there would
    break installs that work today. Both are human-path, not daemon-path.
  - **It closed an unbounded wait while it was there.** The menu bar and the
    notifier ran `agent-notify focus-session` with no timeout at all, on a
    goroutine, so a focus that hung lost that goroutine for good. They now run
    it under 30 seconds — generous on purpose, since core gives each container
    step five of its own and a bound shorter than what it waits on would kill
    focuses that were about to succeed.

- **D-69** (2026-09-21) — **Where agent-notify's files are is answered in one
  place, and an integration never names it.** *Amends* §A10.4 and §A14 (D-20).

  - **There were two implementations of the layout.** `paths` built it with
    `filepath.Abs` and `filepath.Join`; the subscriber SDK, whenever it was
    handed a root outright rather than through the environment, built it again
    by concatenating strings. The `paths` package comment had already written
    down why that is fatal — *a path built in two places is a path that will
    differ in two places* — and the SDK was the second place.
  - **They differed, measurably.** With `AGENT_NOTIFY_ROOT=a/relative/root`,
    letting core read the variable gave
    `/abs/cwd/a/relative/root/config.toml`; passing the same string in as
    `Root` gave `a/relative/root/config.toml`, resolved against whatever
    working directory the process happened to have — which for a child the
    session-watcher started is not the one the person who set the variable was
    standing in. A trailing slash gave `root//config.toml`. Every Fake in every
    test went through the second copy.
  - **Nineteen call sites made the wrong branch the usual one.** Every
    integration wrote `os.Getenv("AGENT_NOTIFY_ROOT")` and handed the answer
    over as `Root`, which looks like a no-op and is not. Two integrations did
    it for `Settings` as well, so the same binary could resolve its own config
    file two ways depending on which helper it called.
  - **`paths.Under(root)` is now the only thing that knows what a root
    contains**, and `FromEnvironment` calls it. A root is made absolute there,
    because a root is a place and not a direction from wherever the caller
    happens to be standing.
  - **The root moved off the free functions and onto the struct.** `Read`,
    `ReadIncludingEnded`, `History`, `ConfigFile` and `CoreBinary` are methods
    on `Integration`, which already carried `Root` and already hosted
    `Settings`. An integration writes `var me = subscribe.Integration{Name:
    Name}` once and calls `me.Read()`. Root stays a field because a test needs
    to name a root without `t.Setenv`, which is mutually exclusive with
    `t.Parallel`.
  - **Emptying the call sites was not enough on its own.** Once the two
    branches agreed, the nineteen `os.Getenv` calls were merely redundant and
    could have been replaced by `""`. That was rejected: `Read("")` documents
    nothing, and the next author to read it puts the `os.Getenv` back as a fix.
    Making the wrong thing unsayable is worth an API break, which the project
    is still free to take.
  - **`WhereEverythingLives` is now `Layout`.** "Everything" named no subject
    and "lives" named no relation, and the giveaway was that every variable
    holding one was already called `layout` — including the receiver. §A7.2 had
    called it the layout all along. `FindWhereEverythingLives` became
    `FromEnvironment`, which says what it reads, and pairs with `Under(root)`,
    which says what it is given. 79 sites, all inside core.
  - **What is left**: 26 `t.Setenv("AGENT_NOTIFY_ROOT")` in six
    `install_test.go` files. An install reaches `ConfigFile` through
    `printTable`, which has no integration to hand it, so the variable is still
    the only seam. It goes when the install skeleton itself is shared.

- **D-70** (2026-09-21) — **A wake-on that can never wake is refused, and the
  SDK is what refuses it.** *Amends* §A10.4 and R23.

  - **The mechanism accepted every likely misspelling.** `Differs` matches by
    string against the encoded record, so a name that is not a field is absent
    from both sides: `""` equals `""`, nothing ever differs, and the subscriber
    is never woken. Measured against the real function, all of `Kernel` (the Go
    field name), `kernal`, `state`, `status` and `what_it_is_doing` let
    `working -> blocked-on-you` pass unnoticed.
  - **The shape of the failure is what makes it worth an error.** Nothing goes
    wrong at startup. The subscriber connects, the handshake accepts it, the
    opening snapshot arrives and the display paints once, correctly. Snapshots
    are only sent on connect, on overflow and on request, so from that moment
    nothing reaches it again. It logs nothing, `doctor` reports it connected,
    and restarting it makes it look fixed — so what a person is left to chase
    is "it is right when I start it and stale an hour later".
  - **The project had already found this and defended one door of two.**
    `TestWakeOnOffersOnlyFieldsARecordHas` says in as many words that "the
    session-watcher matches these names by string and silently never wakes for
    one that is spelled wrong" — and guards the shell-completion list against
    it. Nothing guarded an integration's own `WakeOn`, which is where the names
    actually come from.
  - **The check is in the SDK, not the session-watcher, and this is the whole
    design question.** The watcher already refuses a subscriber for three
    things and has the machinery to refuse for a fourth. But a watcher cannot
    tell a typo from a field added by a newer core, and refusing the second
    would turn §A10.5's additive-only promise into a breaking change every time
    the record grows. The SDK has no such doubt: it is compiled from the same
    source as the record, so its list is exactly the set of names available to
    the person writing the code. The watcher may log an unrecognised name; it
    must not refuse one.
  - **Before the socket.** The check runs beside the two that were already in
    `Run` — no name, no OnChange — so nothing is created, no session-watcher is
    started and no connection is opened by a subscriber that could not have
    worked. A test asserts the root is still empty afterwards.
  - **It names the offender and guesses the intent.** The commonest mistake is
    the Go field name, which differs from the real one only in case, so a
    case-insensitive hit is reported as *did you mean "kernel"?* — worth more
    to the reader than the list of twenty-two fields printed under it.
  - **It does not judge a name that is real and inert.** Naming only `key` or
    `created_at` is a subscriber that never wakes either, but those are settled
    facts about a record rather than misspellings, and a list that also names
    something live is perfectly reasonable. That belongs in `doctor`, which can
    say it without refusing anything.
  - **The other half of this is not done.** A test that publishes a change
    through `subscribe.StartFake` and asserts a repaint would catch the same
    bug from the other side. §A10.4 calls the Fake the highest-leverage item on
    its list; four integrations subscribe and exactly one file in the workspace
    mentions it. The validator stops the typo being writable; the Fake would
    stop it being shippable.

- **D-71** (2026-09-21) — **The notifier stops manufacturing the edge it is now
  handed.** *Completes* D-63, which was built for this one consumer and never
  reached it.

  - **D-63 said so at the time.** Its own comment on `Change` reads: *"a
    notifier is the one display that genuinely needs the transition rather than
    the state (§A12.1) […] Before this, both were decoded and dropped, and the
    one notifier in the world rebuilt a weaker version of them from remembered
    timestamps."* Core then grew `PreviousKernel` and `Event`, and the notifier
    went on reading `view.Sessions` and rebuilding.
  - **What it was carrying**: a `map[string]time.Time` of the `state_since` it
    had last spoken about per session, a `seeded` flag so the first view
    announced nothing, a rule that every LIVE session's clock be remembered and
    not only the announceable ones, and a sweep so the map stayed bounded by
    what was running rather than by uptime. All four exist to answer one
    question — did this move? — that arrives in the view already answered.
  - **Not a bug, and worth saying plainly.** The two rules agree on every case
    that could be constructed, including reconnects, resurrection and a message
    changing while a session is blocked. What was wrong is that there were two
    of them (R24), and they are not the same rule: the notifier's was "the
    record's `state_since` is newer than the one I remember", core's is "the
    fields this subscriber asked about differ from what it was last SHOWN".
    They agree only because a kernel change always moves `state_since`.
  - **The test is now `PreviousKernel != Kernel`**, which is sharper than what
    it replaces. This display asks to be woken for `message`, since the message
    is the body of the banner, so a blocked agent revising what it said arrives
    repeatedly and genuinely changed. The kernel it moved from is the only
    thing that separates "it just blocked" from "it is still blocked and said
    something else".
  - **The memory did not disappear, it moved somewhere that can keep it.** A
    subscriber's picture of what it was last shown survives a reconnection, an
    overflow and a resync — three things this program cannot observe. A display
    holding its own map is correct until one of them happens and cannot be
    tested for any of them.
  - **The file now remembers nothing**, which is the part worth noticing: the
    one place in the system that was allowed state no longer needs it. Its
    logic went from 57 lines to 39, and `Fresh` from a loop with four pieces of
    bookkeeping to twenty lines with three rules, each about the record in hand.
  - **A test was gained rather than lost.** The old suite had to prime the
    notifier with a first view before every assertion, which made "announced
    once" a statement about the fixture. The new one states the real case —
    ten message revisions while blocked produce no second banner — and fails
    when the rule is backed out.

- **D-72** (2026-09-21) — **sketchybar drew the agent's message and did not ask
  to be told when it changed.** A real defect, reproduced, and the first one
  D-70's neighbourhood turned up that was live rather than latent.

  - **What happened.** The preview popup renders `record.Message` through
    `Preview.Of`. This display's `WakeOn` was `kernel detail rank name cwd
    state_since` — no `message` — and the comment above it read *"the message
    changes fifty times in a turn and is not on the bar"*, which was true when
    it was written and stopped being true when the popup was added.
  - **When it bites.** Queue a second prompt at an agent that is already
    working. `UserPromptSubmit` reduces to `Working`, which it already was, so
    the kernel does not change and `state_since` does not move; Claude's
    `UserPromptSubmit` carries no detail. The message moves and nothing else
    this display watched did, so it was not woken and the popup went on showing
    the first prompt for as long as the agent kept working.
  - **Reproduced rather than reasoned.** An end-to-end test against
    `subscribe.StartFake`, with `Sketchybar.Binary` pointed at a shell script
    that records its arguments: publish `working` with one message, publish
    `working` with another, wait for the second to reach the bar. With the old
    list it never arrives and the test times out after fifteen seconds; with
    the fix it lands in under half a second.
  - **The fix is conditional, not additive.** Every path that draws a preview
    is behind `Preview.on()`, and the preview is optional. A display woken for
    something it does not render is a repaint per prompt for nothing (R23);
    one that renders something it is not woken for shows it stale for ever.
    Asking exactly when it will draw is the only answer that is true in both
    configurations, so `WakeOn` became a function of the settings.
  - **This is sketchybar's first test through the SDK.** Its existing tests run
    against the real bar on purpose, because everything they assert is a fact
    about sketchybar. Nothing here is: what is under test is the wiring between
    what this display declares and what it renders, and a recording script sees
    that perfectly.

- **D-73** (2026-09-21) — **A display's `WakeOn` is checked against its own
  renderer, by asking the renderer.** *Generalises* D-72. Adds
  `EachFieldMoved` to §A10.4.

  - **Reading the two side by side works exactly once.** D-72 was found that
    way. The mechanism that produced it — a list of field names kept by hand,
    in a different file from the render that does the looking — was left
    completely intact, in four displays.
  - **Every renderer here is a pure function of records**, which makes the
    question answerable rather than a matter of care: move one field, render
    again, and if the output moved then the renderer reads that field and the
    declaration has to name it. No one has to remember; a line added to a chip
    next year fails the test the day it is added.
  - **Core supplies the awkward half.** `agentnotify.EachFieldMoved(base)` is
    one copy of the record per field, each differing in exactly that field.
    Producing a value that is genuinely different for a time, a kernel, a map
    of raw JSON and a slice of structs is fiddly enough that four repositories
    would get it four subtly different kinds of wrong — and a mutation that
    failed to move anything would read as "my renderer does not use this
    field" and pass, which is a test that is weaker than it looks.
  - **Two mutations had to be made deeper, and the second was caught by the
    sweep failing to fail.** A struct now moves in every one of its fields
    rather than the first that works, and a map CHANGES the entries already in
    it rather than adding a stranger beside them. zellij-display reads
    `captured_context.by["zellij-display"]` and nothing else in that struct;
    the first version of this moved `captured_context.captured_at`, the plan
    came out identical, and removing `captured_context` from the declaration
    did not fail the test. The contract is now pinned in core: every record
    handed back must differ in its own field and in no other.
  - **Settled fields are left out.** `key`, `schema` and `created_at` do not
    move once a session exists, so nothing can be woken by one — and a sweep
    that included `key` would ask every display to wake on it. This is the
    tier D-70 declined to judge, now with a second use.
  - **It found two over-asks while proving itself, in the other direction.**
    macos-bar asked for `message` and has never drawn what an agent said.
    macos-notifications asked for `state_since`, which it read only through
    the remembered-timestamp machinery D-71 deleted. Both are a wake for
    something nobody looks at rather than a wrong picture, and both are gone.
    The sweep does not test for this — one base record cannot prove a field is
    never read, since `ended_at` matters only to an ended session — so it
    stays a thing found by hand.
  - **`WakeOn` became `WhatToWakeFor()` in all four displays**, because a
    literal inside the `subscribe.Run` call is not reachable from a test. For
    sketchybar it had to become a function of the settings anyway.

- **D-74** (2026-09-21) — **The picker's preview reads the clock it was handed,
  all the way down.** *Applies* D-47's "a pure function of the record and the
  clock" to the one place that had slipped.

  - **A test that was true for a day.** `TestTheFactsAreTheOnesWorthScanning`
    asserted `"1d left"` against a fixture whose weekly allowance resets 48
    hours after a fixed noon, and started failing the afternoon that noon
    became more than a day in the past. It was reported as "23h left".
  - **The assertion was not the bug.** `quotaLeft` called `time.Until`, which
    is the wall clock, from inside a render. Everything around it already took
    the time as a parameter — `Preview(record, history, at, width)` threads it
    to `Record.Elapsed` and to `Earlier`, and `Row(record, at, columns)` takes
    it too — so exactly one function was reaching past the clock it had been
    given, and it was the only one whose output could not be reproduced.
  - **The clock is now read once, at the edge.** `now()` in `main.go` and
    nowhere else in the program; `Facts` and `quotaLeft` take an `at`. Fixing
    the string would have re-baselined a test that will rot again; this makes
    it impossible to rot.
  - **Two branches got pinned while it was open**: an allowance whose reset has
    already passed says nothing rather than counting down from zero — what is
    on the record is what was true before the window turned over — and one that
    never reported a reset time says nothing at all, which is the ordinary case
    for the agent that does not report one.

  **[amended 2026-09-22 — D-76]** `quotaLeft` and the test that caught it are
  both gone with the allowance they rendered. The rule they established is not:
  the clock is read once, in `main.go`, and `Facts` takes the record alone
  because there is nothing time-dependent left in it.

- **D-75** (2026-09-22) — **An agent-integration reports a name or nothing, and
  core guesses for all of them.** *Extends* D-60: the name still comes from the
  agent, reported on a hook that was firing anyway, and core still never derives
  `Record.Name`. What changed is which strings count as a name.

  - **A placeholder was being reported as a name.** Claude stamps
    `<cwd-basename>-<counter>` on every session nobody has renamed —
    `agent-notify-14`, `agent-notify-16`, `agent-notify-8e` — and the
    agent-integration reported it, because it was in the `name` field. Nothing
    downstream could tell it from a name a person chose, and the user's habit
    was to rename every session by hand to get anything legible.
  - **`nameSource` says which it is.** Four values: `user` is a `/rename`,
    `auto` is a label Claude generates on the bridge path, and `derived` and
    `collision` are the placeholder. Claude also writes an `ai-title` line at
    the head of the transcript — "Fix Sketchybar display on external monitors" —
    which is neither in the hook payload nor in the session file.
  - **So the claude agent-integration reports the best of three** — the
    `/rename`, then that title, then the `auto` label — **and nothing at all
    otherwise.** An empty name now means *nobody has named this*, reliably,
    which is a fact every display can act on and none could compute before.
  - **The first prompt is not one of the three.** It is always there and it says
    exactly what the session is about, and it is still prose: it would have to
    be cut to fit a tab, and reporting it would take the empty-means-unnamed
    signal straight back again. An agent that has no name for a session says so.
  - **Core's guess gained the id suffix the counter used to supply.**
    `DisplayName` falls back to the last component of `cwd` plus two characters
    of the session id — `agent-notify-8e` — because three sessions in one
    repository would otherwise all be called `agent-notify`. It is per-record
    and stable rather than computed from the rendered set: `DisplayName` is an
    address as well as a label, `agent-notify focus <name>` matches on it, and a
    name that changed with what else was on screen would break focusing and
    flicker in a per-pane renderer. `Record.Named()` reports which of the two a
    display is holding.
  - **The title is dark and is read anyway.** [verified 2026-09-22, 2.1.267] 26
    transcripts on that machine hold one, the newest written 2026-08-27 under
    2.1.231, none in the 222 since. The generator is still in the binary and the
    SDK still documents a custom title as one that "skips automatic title
    generation", so it is a path that stopped firing rather than a field that
    went away — and it costs one read of a file the usage reader has just read
    from the other end.
  - **That read is forwards, once, 64 KiB** — the opposite of the usage reader
    in the same program, for the opposite reason: the title is written at the
    beginning and never rewritten, the numbers are only ever at the end.
    [verified 2026-09-22] it is inside 64 KiB in 18 of the 26 transcripts that
    have one; the other 8 are between 316 KiB and 2.2 MB in, at no fixed
    distance from either end, and are not chased — the scan is unbounded and
    would run on every hook of every untitled session, which today is all of
    them.

  **[amended 2026-10-09 — D-88]** The rule stands and two more kinds of name
  come under it. `nameSource` has six values, not four: `hook` is a name
  somebody chose, and so is a name with no `nameSource` at all. The payload's
  `session_title` is read before all three of the above. `<cwd-basename>-<counter>`
  is `<cwd-basename>-<random byte>`; nothing depended on it being a counter.

- **D-76** (2026-09-22) — **The record stops carrying what no display can read
  across agents: context, quota, and the agent's own namespace.** *Amends*
  §A7.4, §A7.4.3, §A7.4.4 and §A7.7; *retires* `agent_data`.

  - **The test is the one §A7.4 has always set**, and all three failed it in
    different ways. `usage.context` and `usage.context_limit` need a limit, and
    only codex reports one — [verified 2026-09-22] every Claude record in the
    store carries a context level and no limit, so `ContextFilled` has answered
    "cannot tell" for every Claude session ever recorded on this machine, and
    the meter in the picker only ever drew beside codex. `quota` is worse: the
    field is empty for Claude entirely, because its allowance lives in the
    statusLine payload, which is the user's slot (§A7.4.2).
  - **`agent_data` failed it by being unreadable by construction.** It is opaque
    (R7), so the only display that can read it is one that already knows which
    agent wrote it, and no display ever did in three months. It was carrying a
    transcript path, a permission mode, a prompt id and an effort level, and the
    prompt id changed on every prompt — a field that moved constantly for
    nobody.
  - **This is a judgement about the agentic landscape, not about the design.**
    Both were argued for correctly at the time and the arguments are still in
    §A7.4.3 and §A7.4.4; what has not happened is agents converging on a way to
    report either that a second agent can also answer. When two of them do, the
    fields come back as they were written.
  - **What stayed is what both agents fill**: the four token counters, the
    reasoning subset, the cursor, `model` and `branch`. That is the whole point
    of the vocabulary test — the survivors are exactly the fields that passed
    it.
  - **One capability was given up rather than tidied.** A display could reach
    the agent's own transcript through `agent_data.transcript_path`, which §A7.7
    named as the answer to "we are not a transcript store". Nothing followed it,
    but the route is gone, and getting it back means a field of its own.
  - **It cost about 650 lines and returned none.** Half of the codex adapter's
    `usage.go` existed for the `token_count` line; the picker's entire meter
    vocabulary — `meter`, `meterStyle`, `percent`, `quotaName`, `quotaLeft` —
    existed for the two percentages and went with them.

- **D-77** (2026-09-22) — **There is no backwards compatibility, and the
  machinery for it is deleted rather than dormant.** *Amends* §A10.5, *retires*
  R12 and R28, *amends* D-14 and Q14.

  - **D-57 had already established that none of the rules bind.** The module is
    unpublished, `Version` is `0.0.0-dev`, and every integration reaches core
    through a `replace` directive to a sibling directory. What it left standing
    was the machinery, on the grounds that it would be wanted on publication
    day.
  - **Keeping machinery for a promise nobody is owed has a running cost**, and
    it is paid on every change rather than once: three separate mechanisms that
    have to be reasoned about, and a `recordFields` list doing three jobs at
    once so that removing a field forced a choice between resurrecting it and
    breaking D-70's wake-on check.
  - **And in one place it was actively wrong.** R28's round trip preserved every
    unrecognised key on every write, so a field deleted on purpose would have
    been written back for ever on every record that had carried it. D-76 is what
    found this, by trying to remove three.
  - **What went**: the `schema` number, which nothing branched on; the
    unknown-field round trip; the major check at the handshake, which compared
    `"0"` with `"0"` and had never said no; and the claim that an unknown event
    is a newer adapter — it is a mistake in an adapter, and the reducer refuses
    to guess at it for §A5.7's reason rather than a compatibility one.
  - **What deliberately stayed, because it looks like version insurance and is
    not**: the stored urgency-rank and an unknown kernel being live, which
    protect a record already on disk rather than a newer binary — and whose
    `RankUnknown` has a caller with nothing to do with versions at all,
    `Urgency()` over an empty set. And the opaque namespaces, which are R7 and
    R10 rather than compatibility.
  - **The rules in §A10.5 are still the right rules for a published module.**
    They are kept there as a statement, and M18 is where they become code again
    alongside a `Version` the build actually stamps.
  - **It unblocks a tightening nobody has done yet.** D-70 split the wake-on
    check so that the session-watcher would not refuse a field added by a newer
    core. There is no newer core, so the watcher could refuse a typo at the
    handshake instead of leaving a display correct at startup and stale an hour
    later. That is an addition and is not in this decision.

- **D-78** (2026-09-27) — **One duration type, exported and imported rather than
  reinvented, and every duration a person writes is spelled the way people write
  one.** *Amends* D-62, *answers* Q16 with the opposite answer for a reason Q16
  did not consider.

  - **D-62 had the principle right and left the spelling unreadable.** It took
    four bespoke `timeout` settings and core's own `Duration` out, correctly, on
    the grounds that a timeout is mechanism rather than preference — and it
    priced the cost in one line: `keep-ended-sessions = 604800000000000` where
    `"7d"` used to be. The cost it priced was ugliness.
  - **The cost it did not price is that the natural spelling destroys the file.**
    [measured 2026-09-23] `keep-ended-sessions = "24h"` is not a bad value, it is
    a decode error, and `Parse` answers a decode error by discarding the whole
    document — every unrelated key with it — and saying `cannot decode TOML
    string into ... time.Duration`, which names no key and offers no remedy. A
    misspelled key name costs one line. So the plausible mistake was fatal and
    the careless one was not.
  - **And it kept the trap it had itself named.** D-62 observed that
    `timeout = 2` decodes to two nanoseconds without complaint. `announce` on
    both bars inherited exactly that: `announce = 8` is eight nanoseconds, and
    nothing anywhere says so.
  - **The type is a struct, and that is mechanism rather than taste.**
    [verified 2026-09-27, go-toml v2.4.3] go-toml decodes a bare number natively
    into any type whose kind is an integer, so a named `time.Duration` never sees
    it. A struct has no native decoding, so EVERY scalar — number, string,
    boolean — reaches `UnmarshalText`, and one codec owns every message. `0`
    still reads, because `time.ParseDuration` takes it and it means the same
    thing however it is spelled.
  - **Text it cannot read is a complaint, not a refusal.** This is what
    `subscribe/settings.go` already promised in a comment — "a duration that is
    not one", listed among core's notes about a file that was otherwise used —
    and what `problemsWith` already does for every other bad value in the file.
    The code now agrees with the comment.
  - **Q16 asked whether an integration needs core's `Duration` and said no**,
    because core's existed to understand `7d` and no timeout is measured in days.
    The answer is yes now, for a reason Q16 did not weigh: not day units, but
    that nobody should have to know which table they are in to know how to write
    eight seconds. `agentnotify.Duration` is exported, the two bars import it,
    and nothing reinvents it.
  - **No day unit.** A week is `"168h"`. `7d` is precisely what made core's
    spelling disagree with every integration's, and supporting it properly means
    owning a parser and its compounds instead of deferring to the standard
    library.
  - **What it cost**: three keys change spelling — `keep-ended-sessions`, and
    `announce` on both bars — in a file nobody outside this machine has (D-77).
    One bug came out with it: sketchybar's `announce = 0` was documented and
    commented as "the bar stays passive" and was in fact indistinguishable from
    an absent key, so it took the eight-second default instead. The pointer that
    separates absent from `"0s"` is what fixes it.

- **D-79** (2026-09-27) — **agent-notify-sketchybar is deprecated, and deprecated
  by deletion.** *Retires* the module delivered in M14. Does not amend D-37,
  D-51 or D-72, whose reasoning stands.

  - **Unmaintained and still in the tree is the worst of the three states.** The
    module is not published, so it cannot rot quietly in somebody else's copy;
    it is in this repository, and this repository's core breaks its own API
    freely because nothing is published (D-77). An integration left in the tree
    therefore has to be kept compiling on every core change — which is
    maintenance, and is exactly the thing "unmaintained" is meant to stop. The
    choice was never deprecate-or-delete; it was delete, or keep paying.
  - **It already cost something on every run.** Three of its tests drive a live
    `sketchybar` process and fail on any machine without one, so a full test
    sweep across the monorepo has been red for reasons nobody was going to fix.
  - **A banner in the README would not have held.** The rule needed is "exclude
    it from every change", and the things that do not read banners are a `grep`,
    a sweep across `tool-integrations/*`, and a build of everything. Removing the
    directory is the only form of the rule that those obey.
  - **Nothing depended on it.** No production code in any module names it. The
    only non-comment references were test fixtures in core and in the SDK using
    `sketchybar` as the name of an example integration, which is arbitrary —
    core has no registry of integration names and discovers them on PATH.
  - **The references that remain are history and stay.** Thirty-nine mentions in
    this file, and comments across the other modules, record reasoning that is
    still true: D-72 above all, which is the story of a display that drew the
    agent's message and did not ask to be woken when it changed, and which is
    why `WhatToWakeFor` exists in the shape it does. §A19's rule is that a
    decision that was reversed stays here with its reason; the same applies to a
    module that was removed. They are not a to-do list.
  - **What was left unfinished with it**: the preview had an off switch no config
    file could reach — `on()` was false only for dimensions that `sane()`
    refused — so `[…preview]` could never be turned off, and the display always
    declared it read `message`. It is recorded here rather than fixed.
  - **Where it is**: `tool-integrations/agent-notify-sketchybar/` in `827ccc4`
    and every commit before it.
  - **The macOS menu bar is `agent-notify-macos-bar` alone now.** It needs
    nothing installed, which was always the argument for it (M15a).

- **D-80** (2026-09-27) — **The core module is laid out as its dependency graph,
  as far as a directory tree can carry one: `main` at the root, the doors beside
  it, and the depth written down in a test.** *Amends* §A10.4 on where the
  exported surface lives — the same packages, at different paths. Does not
  amend §A10.5, which D-77 has already suspended.

  - **The old layout was the graph upside down.** Measured with `go list`:
    `cmd/agent-notify` was the one package nothing imported, and it sat two
    directories down; the root package `agentnotify` was imported by ten of the
    sixteen and sat at the top. Every intuition the tree gave a reader, it gave
    backwards. `main.go` is now at the module root and the vocabulary is
    `session/`.
  - **`agentnotify` becomes `session`,** at `github.com/lassoColombo/agent-notify/session`.
    It had to move to make room, and the name is better where it is read:
    `session.Record`, `session.Working`, `session.BlockedOnYou`, `session.Reduce`.
    Two members do not fit it — `Version` is the build's and `Duration` is a
    TOML spelling — and they stay rather than earning a package for two symbols.
    Cost: 1,463 call sites across 83 files in all nine modules, and five test
    helpers named `session` that had to be renamed to stop shadowing it.
  - **The command line is nine packages under `command/`,** one per subcommand,
    where it was one package of 2,700 lines. This buys almost nothing in
    ownership terms — the dominator tree goes from two levels to three — and
    that was not the reason. The reason is that `doctor` imports ten of our
    packages and `install` imports none, and while they shared a package there
    was no way to see that, and no cost to the next command importing
    everything because everything was already in scope. What two commands share
    is now under `command/internal/` — `find`, `rows`, `onpath`, `exit` — where
    the sharing is an import somebody has to write.
  - **Which heading a command appears under moved to the tree.** It is a claim
    about who is reading rather than a property of any one command, and it is
    only legible if you can see all four at once.
  - **`internal/branch` is now `hook/internal/branch`.** It is the only package
    in the module with exactly one importer, so it is the only one that can
    honestly nest. **`internal/watcher` is now `internal/sessionwatcher`**,
    which is what this file has always called it in prose, and which frees the
    name for `command/watcher`.
  - **`internal/core` was not renamed.** "Core" is a bare abstract noun and a
    poor name for what the package is — the opened installation, assembled in
    the order that works — but no better one was found, and renaming it is a
    taste call that has nothing to do with the layout.
  - **Depth cannot live in the directories, so it lives in `layering_test.go`.**
    The tree carries which packages are doors and which are behind an
    `internal/`; it cannot also carry depth, because `command/install` (floor 1)
    and `command/doctor` (floor 6) are siblings on disk and `subscribe` (floor
    5) must keep a path an outside repository can love. The test declares every
    package's floor, recomputes it from the source with `go/parser` — build tags
    included, so the check is not only true on the machine it ran on — and fails
    when the two disagree. It also refuses a package nobody placed, a command
    importing another command, and a new exported package that is not one of the
    seven doors.
  - **The seven doors** are `session`, `hook`, `subscribe`, `container`,
    `capture`, `tool` and `logs`. `logs` is one of them because three displays
    write to the shared file (D-79's companion, `827ccc4`); it is easy to
    mistake for machinery and the test now says otherwise out loud.

- **D-81** (2026-09-28) — **One handshake, one interface, no long-lived
  integrations.** Every tool-integration answers `capabilities`, core dispatches
  on the methods it declares, and every integration core knows about is a
  program core RUNS rather than one it keeps. *Amends* §A10.2 (there were two
  lifecycles; there is one), §A10.3 (the declaration channel is a subcommand,
  not the connect handshake), §A14 and D-66 (`capture-environment` has left the
  printed table). *Reverses* D-6 (displays are long-lived subscribers core
  supervises) and the inference half of D-38 (a container is a program that gets
  run — still true — recognised by its absence from `[container] order` — no
  longer). *Retires* D-39, which only existed because the hook could not ask.

  - **What was wrong was one line.** `supervisable()` decided whether an
    integration was a daemon by asking whether it was ABSENT from
    `[container] order`. That key exists to say how shells nest, which §A11.2
    calls the one thing only the user knows — so a property of the program was
    being read out of the user's file, which is the thing §A10.3 and D-66 both
    exist to prevent. It was wrong in both directions: a container left out of
    the order was started as a daemon, and so was the picker, which is a
    terminal program with a binary and no business being started at all. That
    had been happening: started, failed, retried five times, retired, silently.
  - **The declaration channel could not carry it.** §A10.3's answer is the
    connect handshake, and that channel is only reachable by something that has
    already decided to connect. Lifecycle is the one fact needed BEFORE that
    decision exists. So: a subcommand, answered without connecting, before
    anything else is asked.
  - **Capabilities are methods, not roles.** A role is a category a person
    infers and then writes down — in the table, in the order, in a `roles` field
    — and each copy is somewhere it can be wrong. A method is what core is about
    to run. "Is a container" became "answers `focus`", which is not an opinion.
    `roles` was carried, logged, copied into three structs, and branched on by
    nothing; it is gone.
  - **The answer is derived, never written down.** `container.Main` builds its
    own methods list from which functions the author filled in. A nil function
    is already exactly "do not call me", so there is nothing left to keep in
    agreement — and `methods` means "worth calling" rather than "will not
    error", which is a distinction that used to cost a process: an unimplemented
    `focused` was asked anyway and answered "cannot say", indistinguishable from
    a tool that genuinely could not see. With unanimity required (§A11.6), one
    such container made every session on the machine permanently unsure.
  - **`capture-environment` is universal.** Nobody declares it and everybody is
    asked; an integration that reads nothing answers `{}` and core records no
    entry. The bit in the config existed only because the hook has no socket
    (D-39), and it was a fact about the program living in the user's file — yes
    for one that reads nothing, no for one that reads something, no error either
    way. Asking everybody costs what it looks like: the hook only captures when
    there is something new to capture, and runs them concurrently under one
    shared timeout when it does.
  - **A surface that must own a process stops being an integration.** A menu bar
    item dies with the process that made it; a tap on a banner is answered on
    the posting process's main thread. Neither can be a program core runs, so
    both are started by launchd and read `agent-notify tail --json`, which
    §A10.2 already allowed from the other side: an unsolicited connection is
    first-class, and the CLI is a client rather than a second implementation.
    `install` prints the launch agent and loads nothing, for D-66's reason and
    more so — loading one puts a program in your login session for ever.
  - **`binary` means "core may run this", and its absence is a declaration.** A
    table without one belongs to something launchd starts; it is there for its
    settings, and core neither starts it, asks it anything, nor runs it on the
    hook path.
  - **What core knows is written down, because three things read it.** The
    session-watcher asks everyone at startup and on reload — never on a sweep:
    the answer is a property of the program on disk, and rebuilding one is
    already a reload — and writes the answers into `integrations.json`, which
    the supervisor's report became. `doctor` shows it. A focus asked from a
    keybinding reads it instead of running every container to find out what a
    container is, and asks directly when there is no report, so focus still
    works on a machine where nothing is running. The hook reads it too, and
    never asks: R1 owns that path, so it is a file read or the configuration and
    never a process.
  - **`version` finally does something.** Nothing negotiates on it (D-77), but an
    integration that cannot be asked at all is recorded with the reason, and
    `doctor` fails and says to reinstall it. Without that, a display whose
    binary was deleted reports no methods — exactly what a display with nothing
    to offer reports — and core stops running it for ever with nobody told why.
  - **A render is handed its view on stdin.** It cannot read the store instead: a
    process started fresh for one render remembers nothing, and this is the
    field where that matters — a display owns panes it did not create, and the
    only thing that remembers which pane a finished agent had is that agent's
    ended record. So the "since the last view" memory moved into core, one per
    display, where it also survives the display crashing, which it did not
    before. `want_ended` is in the handshake for the same reason: core composes
    the payload now, so core has to be told.
  - **One channel of one, doing two jobs.** At most one render in flight and one
    pending. It coalesces, because a render always paints the whole world and a
    queued one is never worth keeping beside a newer one; and it serialises,
    because two `zellij action rename-pane` in flight together is how a pane
    ends up wearing the wrong name. A render that would carry no changes is not
    run at all, which is what `wake_on` buys now: it used to save a write to a
    socket somebody was already listening on, and it saves a process.
  - **What this deleted.** `supervise.go` entire — the child lifecycle, the
    grace period, consecutive failures, `integration-tries`, the backoff, and
    `whoIs`, which existed only to guess which connection belonged to the child
    core had just started. `subscribe`'s five hundred lines of socket protocol
    went to `internal/subscriber`, because no integration links it any more and
    the socket has one consumer shape: `tail`. Net, across the eight steps, a
    thousand lines fewer.
  - **The picker's path is in its keybinding, and nowhere else.** It was the one
    thing left open, and the answer fell out of the macOS two: they became
    clients with no path in their tables at all — theirs is in the launch agent
    — so there was no second client wanting a second key, and no second key.
    Its table is a heading and its settings now. Its `enabled = false` went with
    the binary: that was never a statement, it was the only way to say "do not
    start this" to a rule that said "started if enabled and has a binary", and
    it said it by calling a thing that was on off.

- **D-82** (2026-09-28) — **The session-watcher sends whole worlds, and what
  changed is worked out by whoever has the memory.** The socket carries `hello`,
  `welcome`, `refused` and `snapshot`, and nothing else. *Amends* §A12.2 (a
  resync is not a first-class path, it is no path at all), §A13.1 (a delta was
  the steady state; a snapshot is), §A9.3 and R15 (a backlog does not degrade to
  a resync — there is no backlog), and §A7.4.3 (one gate, not two). *Retires*
  the `event` half of D-12's delta and the `subscriber-queue` setting.

  - **Two implementations of one thing.** D-81 gave a display core RUNS a whole
    `View` on stdin: one cap-1 channel, `LastShown.Replace`, hand it over, 171
    lines. The socket did the same job in 843 — deltas, a bounded queue per
    subscriber, coalescing by session key, and a degradation path that threw the
    queue away and sent a snapshot on overflow. A snapshot was already the
    answer to a cold start, a reconnection and an overflow; making it the answer
    to the ordinary case too deleted the other four mechanisms.
  - **`event` went the whole way and nobody looked.** An agent-integration
    reported it, the hook put it in a datagram, the session-watcher held it in a
    map until the next sweep, and it was handed to every display in
    `View.Changed`. Its only reader in the system was `replay`, writing it to a
    recording so it could play it back into a fake and have it come out the
    other side. The notifier it was for tells a transition from a nudge by
    comparing kernels — a fact about the record, not a claim somebody made about
    it. Renderers never got one at all, which is the tell: the two display paths
    already disagreed about whether "why" existed and nothing noticed.
  - **`gone` and `resync` answered questions a whole world answers.** A map
    rebuilt from the world it was just sent cannot keep a pruned session. And
    `resync` existed for a display restarted under core — `sketchybar --reload`
    was the case, and sketchybar went in D-79 — where every write is a fresh
    snapshot now.
  - **What changed is computed at the end that survives.** This is the one place
    the collapse stops. A display core RUNS is a fresh process that remembers
    nothing, so core keeps its `LastShown` and fills in `Changed` for it. A
    client on the socket outlives both its own connection and this
    session-watcher, so it keeps its own and fills in `Changed` for itself —
    which is why an agent that moved while the daemon was restarting is still
    reported as having moved. Hand that job to the server and the fact dies with
    the process that knew it. So the socket carries the world, and a view — the
    world plus what it means to you — is made where the memory is.
  - **The server keeps a second, narrower one.** Per connection, answering "is
    this worth writing" and never "what changed". Without it `wake_on` stops
    meaning anything on the wire: a subscriber that asked about kernels alone
    would be written to every time an agent spends a token (R23).
  - **One gate, asked where the answer is knowable.** `WorthOfferingAround` was
    the generous question — "could ANYBODY want this?" — that the
    session-watcher asked once before asking each subscriber the narrow one.
    With every display comparing the world it is offered against the world it
    last took, on the fields it named, the narrow question is the only question
    and there is nowhere left to put a gate that answers it on somebody's
    behalf. The outcome is unchanged: a usage-only write still reaches exactly
    the displays that named `usage`.
  - **It found a bug that predates it.** A session LEAVING the world is not a
    change to any record — there is no record any more — so a display woken by
    a prune compared everything it held against everything it was given, found
    them identical, and drew nothing. A bar kept the row of an agent that
    finished ten minutes ago, and `prune`'s "wake them, this one does not come
    through as a record moving" comment was describing a wake-up that then
    decided nothing had happened. `Replace` now reports a departure separately
    from a change, and both display paths ask about both.
  - **What it cost.** Two writes in the same instant to different sessions used
    to be two messages and are now one world carrying two changes. That is
    coalescing working, not a loss: `Changed` means "since the last view" and
    says so accurately.


- **D-83** (2026-09-28) — **The store is the bus, and one SDK answers every
  subcommand.** There are no sockets. A hook writes its record and makes sure a
  session-watcher holds the lock; the session-watcher and every display that
  owns its process watch `state/sessions` and `state/ended` with kqueue and
  re-read the store when an entry moves. *Amends* §A13 (there is no IPC beyond
  files and exec), §A9.2 (no sockets to unlink and rebind; the lock alone),
  §A12.2 and D-82 (a world is read, never sent), §A10.2 (an unsolicited
  subscriber is a program that watches the directory). *Retires* R15 (there is
  no backlog), D-19's poke, `record`/`replay` and `watcher status`. *Reverses*
  D-37: zellij is one program, `agent-notify-zellij`.

  - **Both channels had become the filesystem's own notification, spelled
    twice.** Since D-82 the poke carried nothing and the stream carried whole
    worlds every reader diffed for itself, so each said "something in the store
    changed", which is what a directory watch says. Deleting them deleted
    `poke.go`, `subscribers.go`, `internal/subscriber`, `session/protocol.go`,
    `subscribe/client.go`, the socket-length checks and doctor's socket probe;
    what arrived is `DirWatch`, fifty lines beside `Exits` on the same kqueue
    call, and `subscribe.Run`, which is the loop `tail` and both macOS
    displays share. The macOS displays link it rather than forking
    `agent-notify tail --json` and parsing its stdout.
  - **The read path was already the truth (R4).** A display reads through
    `WhatIsRunning`, which applies liveness as `list` does, so a client and a
    cold read can no longer disagree about a killed agent.
  - **One SDK.** `subscribe.Main(integration, commands, args)` answers
    `capabilities` (derived from which functions are filled in), `capture-
    environment`, `render` and the three container verbs, then the program's
    own subcommands. `container.Main` went; `container` is the vocabulary. The
    `.app` bundle writer, the launch agent and the tool-path check
    (`tool.AbsolutePath`) are core's, because five programs had copies.
  - **The hook asks the config, not the report.** Every runnable integration
    answers `capture-environment`, so `integrations.json` bought the hook
    nothing; `focus` and `doctor` still read it.
  - **zellij is one program.** Of D-37's four reasons for two, two died with
    D-81 (nothing is supervised or retired), one with the monorepo (a shared
    library is a package), and the last — titles without focus — is leaving
    `zellij` out of `[container] order`. One capture instead of two on every
    session start, one zellij client instead of two.
  - **The store is the only stamper.** `session.Apply` no longer sets `Key`,
    `Sequence`, `CreatedAt` or `UpdatedAt`; the store did already, on every
    write, and the tests that read them off the pure reducer now read them
    off the store.

- **D-84** (2026-09-28) — **Simplification: one world, one shape, one place
  for what every integration copied.** The architecture review of the same day
  found no fault with the bus and eighteen places where the code around it did
  the same thing twice or kept the scaffolding of a mechanism D-83 retired. All
  of them are closed; nothing was added and no module was merged. The working
  plan is [simplification-plan.md](../simplification-plan.md). *Amends* §A9.1
  (the watcher keeps no picture of the world; it reads the store), §A10.4 (the
  SDK owns install, bundle install, an agent's main and the transcript reader),
  §A7.4.1 (an empty capture is stored). *Retires* the floor table of
  `layering_test.go`, `capture` as a package, `Capabilities.Answer`,
  `internal/subcommand` and `command/internal/exit`.

  - **One world.** The watcher's renderers read `Core.WhatIsRunning`, the
    same read every other display and `list` make, with liveness applied. The
    in-memory `known` map served renders a world without it, so a killed agent
    was `ended` on the menu bar and `working` in zellij until the next sweep.
  - **One shape for capabilities.** `session.Capabilities` is what is asked,
    what `integrations.json` carries, and what `containers.Configured` and the
    renderers read; a program that could not be asked has a `problem` beside
    it rather than a second type. The watcher's wake channel holds one wake,
    so the record it writes from `derive` costs one empty `reconcile`.
  - **An empty capture is stored.** `worthCapturing` compares who was asked
    with who answered; an integration that answers `{}` because it reads
    nothing is an answer, and one that failed is not. Before, the designed
    empty answer was dropped, so the day a runnable integration had no `Reads`
    every hook forked every integration again.
  - **`Core` is opened once per process** and kept by `subscribe.Integration`,
    which carries the configuration's problems, so a picker's preview no
    longer creates six directories and parses the file per row. `watcher run`
    resolves its layout from the environment the spawner kept, which is what
    the kept environment was for; the three flags that duplicated it are gone.
  - **The daemon is the daemon.** `internal/onewatcher` is the lock and the
    spawn; `internal/storewatch` is the kqueue on the store; `hook` and
    `subscribe` import those and not the process.
  - **What every integration copied is in core.** `Integration.Install` and
    `Integration.InstallBundle` are the two install shapes; `hook.Main` is an
    agent-integration's dispatch; `hook.LastResponses` reads a transcript
    backwards for what the last responses cost;
    `session.FieldsRenderedButNotWokenFor` is the D-73 test's body. Every
    tool-integration reads its settings through `Read(me)`, lazily, so
    `install` runs against a broken file.
  - **`subscribe.Main` refuses a `WakeOn` naming no field and a `Named`
    subcommand that shadows a verb core runs**, because both failed silently.
  - **The prose describes the bus that exists**: no socket, datagram, poke or
    subscriber survives in a comment, and `tail` derives its completion from
    the record. `//go:build unix` is off files whose only implementation is
    darwin; the XDG branches in `paths` stay because M17 is still planned.
    `container` stays a package, as D-83 chose.

- **D-85** (2026-09-29) — **Integrations file what only they know, in files
  they own; two commands set a machine up.** *Amends* D-66: the file the user
  edits is still never written by any program, and a table an integration
  derives from its own binary is that integration's to file, in
  `conf.d/<name>.toml` beside `config.toml`, read first with the user's file on
  top. *Amends* §A14 (the configuration is the drop-ins and the user's file),
  §A12 and D-52 (a resident display's `install` writes and loads its launch
  agent; re-running it is the upgrade, and the reload waits for the old job to
  go, because `bootout` returns before launchd is done and a `bootstrap` inside
  that window fails and leaves nothing running), and `[integration.<name>]`
  (gains `launch-agent`), and §A3.1 (*reverses* "nothing is a Go workspace":
  there is a `go.work`). *Keeps* `[container] order` the user's: the one fact that
  is not discoverable stays hand-written, and was offered as derivable and
  declined.

  - **The rule over-reached.** D-66 protected `config.toml` and took with it
    the absolute path of a binary, the path of the tool beside it, the
    signing identity a bundle was built with and the launch agent that keeps
    a display alive: facts no person can know better than the program, which
    every install printed and every person retyped, and three of which no
    install printed at all. The install path was eight builds, seven
    installs, nine hand-written tables and two launch agents copied out of
    stderr. It is `make install` and `agent-notify install`.
  - **A drop-in is a file, not a stanza.** Writing into `config.toml` would
    mean a TOML rewriter that keeps a person's comments, which go-toml does
    not do, and seven programs touching a file they do not own. A file per
    integration is written whole, replaced whole and removed whole, and the
    user's file wins by being read last.
  - **The agent's process name is the agent-integration's fact.**
    `[agent.claude] binary = "claude"` was the one table no install produced,
    without which liveness never works and with which `doctor` stopped
    complaining; `agent-notify-claude install` files it.
  - **Core files itself.** `agent-notify-binary` is where `agent-notify` is,
    which `agent-notify install` knows; it goes in `conf.d/agent-notify.toml`
    before any integration is handed the terminal.
  - **`uninstall` is `install` backwards**, for core and for every
    integration: hooks out of the agent's file, the drop-in gone, the launch
    agent stopped and removed, the bundle removed.
  - **`doctor` checks the first-hour failures**: that `agent-notify-binary`
    resolves, what `containers.Configured` would complain about, an agent with
    sessions and no table, and whether each launch agent is loaded.
  - **A `go.work` and a `Makefile`.** §A3.1 refused a workspace when these
    were ten repositories a contributor might clone one of; they are one clone
    now and the `replace` directives still say where core is, so the reason
    went with the repositories. Workspace mode does not make `./...` span
    modules, so the Makefile walks them, honours `GOBIN`, and says where it
    put things and whether that is on your PATH.

- **D-90** (2026-10-09) — **A capture is replaced per integration, and only
  what was derived from that integration's entry goes with it.** *Amends*
  §A7.4.1 (the lifetime of `captured_context` is per entry) and D-84's "an
  empty capture is stored" (the hook no longer compares who was asked with who
  answered; it asks whoever has no entry). Found by the architecture review of
  the same day, `architecture-review-2026-10-09.md`.

  - **What was wrong.** `worthCapturing` recaptured whenever the set of
    integrations that answered differed from the set that would be asked, and
    `session.Apply` replaced `captured_context` whole and set
    `derived_context` to nil. So aerospace timing out once made the next hook
    run zellij's capture again, and the session-watcher run `zellij action
    list-panes` for it again, up to three attempts, for a session whose
    zellij answer had not moved. One integration's failure cost every other
    container its placement, on every hook, until the failure stopped.
  - **The rule is one, and it is `Record.CaptureIsStale`.** An agent's
    environment does not change while its process runs, so a capture is
    stale in two cases only: nothing captured yet, or a different process. The
    hook asks everybody when it is true and only the integrations with no
    entry when it is false; `Apply` replaces the whole capture when it is
    true and merges per name when it is false, voiding `derived_context` for
    exactly the names that arrived. Both read the same method, so they cannot
    disagree about which case they are in.
  - **What a top-up leaves alone.** `captured_at` and `ancestry` date the
    snapshot of this process and stay; an entry for an integration that has
    since been removed from the configuration stays too, because nothing it
    read has changed and nothing asks for it.
  - **The invariant §A7.4.1 wanted is kept, per key.** A derived entry never
    outlives the capture it came from; it is still one atomic record write,
    and no reader compares timestamps.

- **D-91** (2026-10-09) — **`interpret-environment` runs beside the loop, not
  on it.** *Amends* D-84's "the record it writes from `derive` costs one empty
  `reconcile`", which is now how derivation reaches the displays rather than a
  side effect. Found by the architecture review of the same day.

  - **What was wrong.** `reconcile` called `derive` synchronously, and
    `derive` runs each container's `interpret-environment` under a 5s timeout,
    up to three attempts per (session, container). While zellij was asked
    about a pane, the one-slot wake channel held whatever arrived and nothing
    was drawn: an agent that exited, or finished a turn, waited on a tool
    that D-38 had put off the hook's path precisely because it is slow.
    `TestASlowContainerDoesNotDelayADraw` shows a 3s container holding a draw
    for 2.5s.
  - **Derivation is a goroutine with a one-slot ask channel, the same shape
    as the wake channel.** `reconcile` asks and goes on; the goroutine reads
    the store, derives what has a capture and no coordinates, and writes each
    answer through the store. That write wakes the loop, which draws. No new
    mechanism: the store was already the bus (D-83), and the loop already
    survived a wake it caused itself.
  - **What the goroutine reads is narrow.** `[container] order` resolved
    against the capabilities answers, under the one mutex, set at startup and
    on reload; never `Settings`. The refusal counter already lived there. The
    store is written from two goroutines the way it is written from two
    processes, under flock per record, and the update still refuses to attach
    coordinates to a capture that changed while it ran.
  - **Shutdown waits for a derive in flight**, at most one `interpretTimeout`,
    before closing the store and the lock.

- **D-92** (2026-10-09) — **The session-watcher reads and judges once per
  wake, and hands that world to every display it runs.** *Amends* D-84's
  "One world", whose renderers each read `Core.WhatIsRunning` for themselves,
  and D-30's "`list` applies the liveness decision", which is now the same
  decision the watcher files. Found by the architecture review of the same
  day.

  - **What was wrong.** `reconcile` listed the store and probed every live pid
    through `process.Ended`; then each renderer, on its own goroutine, called
    `WhatIsRunning`, which listed the store again, read the boot identity
    again and probed every pid again through `LivenessOf`. N displays cost
    N+1 reads and N+1 sweeps per wake, and two liveness functions existed:
    the watcher's knew about superseded sessions and had the rail that
    refuses to judge when it cannot see its own process; the readers' did
    not.
  - **One read, one decision.** `Core.WhatIsRunningAndWhatEnded` is the read
    every display and `list` make, with the verdicts beside the records for
    the one caller that files them. It judges with `process.Ended`, so
    `list` and a display reading cold now show a superseded session as
    ended, as the watcher was about to file it. `WhatIsRunning` is that read
    without the verdicts, and `LivenessOf` has one caller.
  - **The renderers are handed the world, not a function.** `reconcile`
    reads once, ended sessions included when any renderer asked for them,
    ends what the verdicts name, and wakes every renderer with the same
    slice. A renderer keeps the newest it was given and paints that; one
    that did not ask for ended sessions drops them as it renders. The
    world already says `ended` where the store is about to, so the
    watcher's own write costs one pass that draws nothing.
  - **What went.** The watcher's cached boot identity and prober, read in
    core per call as `list` always did; and the cold-path wake in
    `startRenderers`, because the reconcile that follows a start or a reload
    hands a new display its first world.
  - **Not changed.** `subscribe.Run` reads for itself, once per wake: a
    display that owns its process has nobody to be handed anything by.

**The payload discussion of 2026-09-17 is now ratified**
 in D-10 through D-18.
What is still marked [proposed] elsewhere — the field list of §A7.4, the event
list and reducer of §A5.7, §A9.3, §A10.2–A10.3, §A11.5–A11.6, §A12.1, §A13.1 —
is implementation detail to be confirmed while building, not unanswered design.
§A5.5–A5.9, §A7.4, §A7.6, §A7.7, §A9.3, §A10.2–A10.3, §A11.5–A11.6, §A12.1 and
§A13.1 are all marked **[proposed]**: worked out, written down so they are not
lost, and not yet agreed. They become D-10 onward when they are ratified, one
entry per decision, with the reversals named as D-4 and D-6 name theirs.

---

# B. Plan

- **D-86** (2026-10-07) — **The notification is somebody else's bundle, and this
  display becomes an ordinary one.** *Reverses* the implementation half of D-53
  and D-54 — the `.app`, the identifier, the signing identity, the icon, the
  copy of the binary and the Objective-C — and *amends* D-85 (this integration
  no longer files a launch agent) and D-81 (`binary` is back in its table,
  because core may now run this). *Keeps* every measurement D-53 recorded: they
  are all still true, they are simply no longer ours to live with.

  - **What D-53 found was never about notifications, it was about identity.**
    macOS will not take a notification from a process without a bundle it has
    registered; an ad-hoc signature is refused silently; and a decision it makes
    about an identifier cannot be unmade — two were burned finding that out. The
    cost of owning an identity was a `.app` holding a COPY of the binary, a
    self-signed certificate made by hand with openssl, a reinstall after every
    rebuild, and a hazard that could not be undone. `alerter` is a bundle
    somebody else maintains, signed and notarised by Apple, installed with brew.
    Posting through it is an exec.
  - **The embedded Info.plist was tried first and does not reach this far**
    [measured 2026-10-07, macOS 26.6.2]. A bare Go binary with an
    `__TEXT,__info_plist` section does get a real `bundleIdentifier` and a
    preferences domain that SURVIVES the process — written on one run and read
    back on the next, out of `~/Library/Preferences`. It does not get a
    notification: `UNUserNotificationCenter` still aborts with
    `bundleProxyForCurrentProcess is nil`, because what it wants is
    LaunchServices registration of a bundle on disk and not a plist in a
    binary. **So the trick is no use here and may be the whole of the menu bar
    display's bundle**, which exists for the preferences domain alone (D-52).
    Recorded here because the measurement was taken here.
  - **The three facts that decide the shape** [measured 2026-10-07, macOS
    26.6.2, alerter 26.5]:
    - **A second banner in the same `--group` reaps the first.** The superseded
      alerter exits by itself, printing `@CLOSED`. So nothing tracks children:
      one banner per session, replaced rather than stacked, is what `--group`
      already means, and D-54's requirement is met by somebody else's code.
    - **A detached alerter outlives whatever started it**, reparented to pid 1,
      still holding its banner. That is what makes this a render rather than a
      resident process.
    - **A tap is `@CONTENTCLICKED` on stdout**, and the close button, a timeout
      and being replaced are three other words. Only the first focuses
      anything: the other three are somebody declining to be interrupted, and
      acting on them would take you to a session you had just dismissed.
  - **The last one dissolves D-54's hardest finding.** "A program that posts is
    not therefore a program that can be talked to" was true, and the reason
    this display owned its process and launchd owned the display: a tap is
    delivered to the poster, on its main thread, so a poster that had exited
    left banners nobody could click. The poster is alerter now. What has to
    stay alive is a detached child of this program holding a pipe, and the way
    back is a line of text rather than an AppKit run loop — so there is no
    `NSApp`, no main thread to lock, no run loop to prove is draining, and no
    launch agent. `binary` goes back in the table and core renders this like
    any other display.
  - **The sprite moved to `image/png`** and the module now builds with
    `CGO_ENABLED=0`. Nine rows of squares and a rounded tile are not a reason
    to carry a cgo toolchain, an Objective-C file and the Xcode command line
    tools as a build dependency — they were only ever free because Cocoa had to
    be linked anyway.
  - **What it costs, and it is not nothing**: a dependency that is not in this
    repository and that a person has to install; banners that arrive under
    ALERTER's identity, so System Settings files them under its name and the
    per-app controls are its own rather than agent-notify's; alerter's default
    of impersonating `com.apple.Terminal`, which is left alone; and one process
    per outstanding banner, bounded by the number of live sessions and reaped
    by the next banner for the same one.
  - **What it buys**: 489 lines of Objective-C and the cgo wrapper over them gone, no bundle, no
    `codesign`, no keychain identity, no `iconutil`, no launch agent, no copy
    to go stale, no reinstall after a rebuild — `make install` is the whole
    upgrade — and no identifier of ours for macOS to make an irreversible
    decision about.

- **D-87** (2026-10-07) — **`agent-notify-macos-bar` is removed, and the macOS
  display that survives is renamed `macos-notifier`.** *Retires* the module
  delivered in M15a. *Amends* D-54 (there are no longer two macOS displays to
  keep apart), D-52 (the `.app` it reasoned about exists nowhere in this tree
  now) and D-85 (nothing here files a launch agent any more). Does not amend
  D-51 or D-72, whose reasoning about what a paint costs and what a display may
  wake for stands and is why `WhatToWakeFor` has the shape it has.

  - **The same argument as D-79, and it is the owner's to make.** The bar cost a
    signed `.app`, a launch agent, a copy of the binary that went stale on every
    rebuild, and 820 lines of Objective-C — in a repository whose owner does not
    want to maintain a bundle and does not write Objective-C. Unmaintained and
    still in the tree is the worst of the three states: nothing here is
    published, so core breaks its own API freely (D-77), and a module left
    standing has to be kept compiling on every change. The choice was delete or
    keep paying.
  - **There was a way to keep it that was not taken, and it is recorded so that
    nobody rediscovers it as an oversight.** The bundle existed for exactly one
    thing — `NSStatusItem.autosaveName` needs a preferences domain, and a
    bundle-less process has none (D-52) — and D-86 measured that an embedded
    `__TEXT,__info_plist` section gives a bare binary a domain that survives the
    process. So the `.app` was probably removable. It was not pursued, because
    the Objective-C was the other half of the complaint and because removing
    the bundle would have left the larger cost in place.
  - **What is lost, and it is not nothing.** The bar was the only LEVEL this
    system showed: an ambient "three working, one blocked" that is true whether
    or not anybody asked. A notification is an EDGE (§A12.1, D-53) and answers a
    different question. Nothing in this repository now says what every session
    is doing without being asked — `picker` says it when you press a key,
    `zellij` says it on the tabs you happen to be looking at. That is a
    capability gone, not a capability moved.
  - **What it left dead in core went with it**, on the same day and for the
    same reason. `subscribe`'s `BundleInstall`, `InstallBundle`,
    `UninstallBundle`, `DefaultBundle`, `Bundle` and `LaunchAgentPlist` existed
    for a display that owns its process and lives in a bundle, and there is no
    longer one — they were used by nothing but their own tests. So
    `subscribe/bundle.go`, `subscribe/launchagent.go` and `subscribe/bundle_test.go`
    are gone, `subscribe/install.go` is 95 lines where it was 306, and what is
    left is `Install` and `Uninstall`: a drop-in written, a drop-in removed.
    **This reverses the half of D-85 that is about resident displays** — an
    install no longer writes or loads a launch agent, because nothing here has
    one — and *amends* `[integration.<name>]`, which **loses `launch-agent`**
    (D-85 added it). `doctor` no longer asks launchd anything. A config file
    still carrying `launch-agent` will be reported as an unknown key, which is
    the right way to find out.
  - **The rename reaches the configuration.** Core derives an integration's
    program as `agent-notify-<name>` (`command/install`), so renaming the module
    renames the integration and therefore its table: `[integration.macos-bar]`
    is gone and `[integration.macos-notifications]` is now
    `[integration.macos-notifier]`. A machine that had either removes the old
    drop-in and runs `agent-notify install macos-notifier`. The name is the
    honest one: this is a notifier, and "notifications" named the surface rather
    than the program.
  - **Where it is**: `tool-integrations/agent-notify-macos-bar/` in `ab3dbed`
    and every commit before it.

- **D-88** (2026-10-09) — **The title a session was given is read from the
  payload first, and a name a hook chose is a name.** *Amends* D-60, whose "the
  hook payload could not have carried it" stopped being true on 2026-09-20, and
  D-75, whose four values of `nameSource` are six and whose placeholder ends in
  a random byte rather than a counter. Does not amend D-75's rule — an
  agent-integration reports a name or nothing, and core does the guessing —
  which this applies to two more kinds of name.

  - **Three kinds of name were being dropped.** [verified 2026-10-09, 2.1.285]
    by driving six interactive sessions in tmux against a store of their own,
    with `agent-notify-claude` built from the tree. A `sessionTitle` returned by
    a `UserPromptSubmit` hook is filed with `nameSource: hook`, which the
    integration did not know. One returned by a `SessionStart` hook is written
    to the transcript and never to the session file, whose `nameSource` stays
    `derived`. And a session started with `CLAUDE_CODE_SESSION_NAME` gets a file
    with a name and no `nameSource` at all. All three were listed under core's
    fallback: `p5-upstitle-c7` for a session called `ups-named-probe`.
  - **The payload is read first.** `UserPromptSubmit` and `SessionStart` carry
    `session_title`: 910 of 47,742 captured payloads, on those two hooks and no
    other, and only ever a title somebody gave — `/rename`, `--name` or a hook's
    — never the placeholder and never the `ai-title`. The objection recorded in
    `sessionname.go` on 2026-09-22, that it arrives on a prompt and not on the
    tool call after it, does not hold: an empty name leaves the stored one alone
    (D-60), so a name reported once stays. It is also the only name there is on
    a `SessionStart`, which runs about 37 ms before Claude writes the session
    file, so until now the first record of a `--name` or resumed session was
    unnamed even though its payload said what it was called.
  - **`hook`, and no `nameSource` at all, count as chosen.** The second is a
    name the binary itself labels `user` and the file leaves the label off.
    Claude strips `CLAUDE_CODE_SESSION_NAME` from a hook's environment, so the
    file is the only place that name exists. The risk is a Claude that writes a
    placeholder with no source, and none in reach does: `nameSource` was there at
    2.1.236.
  - **What stays as it was.** `custom-title` in the transcript is not read: the
    payload and the file between them carry every named session the probes
    produced, and a third read would add nothing. `ai-title` is still read and
    still dark — none of the 206 transcripts on this machine holds one, and a
    probe session driven through a prompt wrote none. `collision` stays a
    placeholder: it is behind `tengu_session_name_uniqueness`, which is off on
    this machine, so two sessions given one name both kept it and the case could
    not be seen. `peer` was not traced.
  - **The placeholder's suffix is `randomBytes(1)` in hex, not a counter.** Two
    sessions started in the same second were `-4a` and `-2a`, and none of five
    matched a hash of its session id. Nothing depended on it being a counter,
    and the word is corrected where it was written.

- **D-89** (2026-10-09) — **On Linux a process's start time is its time since
  boot, not a date.** *Amends* §A8.2, whose Linux line named field 22 of
  `/proc/<pid>/stat` and left unsaid how it becomes a `time.Time`.

  - **The obvious conversion is the one D-29 refused on macOS.** Field 22 counts
    clock ticks since boot, and a date is that plus `btime` from `/proc/stat`.
    But `btime` is not stored: it is computed on every read as the wall clock
    minus the time since boot (`getboottime64`, `offs_real − offs_boot`), so it
    moves whenever the clock is stepped — NTP correcting a large offset, a
    laptop waking with a drifted clock, `date -s`. It is also whole seconds, so
    a step of a fraction of one can move it by one. A start time that moves by
    a second makes `SameProcess` call every live session a stranger and ends
    all of them at once, the failure §A8.3 calls the worst this system could
    have. **[from the kernel source, not probed]**: the only Linux at hand was
    Docker Desktop's VM, whose clock a running database shares.
  - **So it is kept as the kernel counts it.** `Facts.StartedAt` on Linux is
    `time.Unix(0, 0)` plus the ticks, at `USER_HZ`, which is 100 on every
    architecture Go builds for. It stays a `time.Time` and nothing that compares
    it changes: it is only compared with another reading from the same machine,
    and a different boot is ruled out by the boot identity before any start
    time is read.
  - **What it costs.** `started_at` in a Linux record reads as a moment in
    January 1970, and an integration that compares an ancestor's start time with
    its own probe must read `/proc` the same way. No display reads
    `session.Process`, and the one integration that reads ancestors,
    aerospace-container, is macOS's.
  - **Rejected: a field of its own, or an opaque start token.** Either moves the
    difference out of `facts_linux.go`, where it is one line, into the record
    and every test that builds one.

## B1. How this works

- The entries below are **meta-steps**. Each is expanded into its own concrete
  plan when we reach it; some need exploration first, some are small enough to
  do directly.
- **The later a step is, the less detail it carries, deliberately.** Planning
  M16 today would be planning against a system that does not exist yet.
- A step is finished when its **done when** line is true — something that can be
  run and observed, not code that looks right.
- Numbers never shift. A completed step stays, marked with its date; a new one
  is appended, or inserted as M6a, exactly as the question numbers in §A18
  behave.

## B2. What drives the order

- **Every step ends at something you can run.** No step in this list delivers
  only a library.
- **Front-load what is expensive to change** — the record schema, the store's
  write discipline, liveness — and defer what is cheap to add, which is more
  displays and more agents. A wrong schema costs a migration across
  repositories we do not control; a missing display costs an afternoon.
- **Probe before planning.** A step marked *needs exploration* has its unknown
  settled by a throwaway probe before its concrete plan is written, the way
  `kqueue` and `SOCK_SEQPACKET` were settled on 2026-09-16.

## B3. Foundations

### M1 — Toolchain and repository skeleton — **done 2026-09-17**

- **Delivered** `.tool-versions` at Go 1.26.2, `go.mod` at
  `github.com/lassoColombo/agent-notify`, a README, and the public-surface
  boundary drawn: the root package `agentnotify` is the entire API an
  integration may import, and everything else will live under `internal/`
  (§A10.4).
- **Done**: `go build`, `go vet` and `go test ./...` are green, and the built
  binary runs on macOS 26 — the Go 1.21 `LC_UUID` failure does not recur.
- **One correction to what this step assumed.** There is no stale `GOROOT`
  export to remove: `~/.config/nushell/config.nu` sources asdf's
  `set-env.nu`, which re-runs `asdf which go` on every prompt and therefore
  follows a directory's `.tool-versions` correctly. What is stale is only the
  environment **inherited by a process launched from a shell that had it set** —
  a Claude Code session, a CI runner, a script. The pin does not win against an
  inherited `GOROOT`, so anything building outside an interactive nushell prompt
  wants `env -u GOROOT`. Recorded in the README.

### M2 — Paths and configuration — **done 2026-09-17**

- **Delivered** `internal/paths` (both directories per platform, XDG honoured,
  `AGENT_NOTIFY_ROOT`, mode 0700 enforced past the umask, the socket-path check),
  `internal/config` (TOML with defaults, a `Duration` that understands `7d`,
  graded failure), `internal/logs` (the shared log, UTC, tagged by component),
  and `agent-notify doctor`.
- **Done**: `AGENT_NOTIFY_ROOT=<tmp> agent-notify doctor` puts state, runtime,
  configuration and log under that root and nothing outside it; a malformed file
  yields defaults, one complaint naming the line and column, and one line in the
  log at mode 0600. 20 tests, `go vet` clean.
- **On the "exit 0" half of the done-when.** doctor exits **1** when a check
  fails, deliberately: a health check nothing can branch on is a health check
  nobody runs twice. The exit-0 guarantee belongs to the hook path, and is
  structural rather than tested here — `config.Load` has no error return at all,
  so no caller *can* fail on a bad configuration. The end-to-end proof arrives
  with the first process that exits on the hook path, in M6.
- **Failure is graded, which the design had not said.** A file that does not
  parse is refused whole, because a half-decoded file silently mixes the user's
  intent with defaults in a way nobody can see. A file that parses but says
  something unrecognised keeps everything it got right: one typo costs the user
  that key, not the other twelve lines. Unknown keys are found by a second,
  strict decode — a strict decoder alone would decide how much of the file
  survives based on the order the user happened to write it in.
- **Beyond the brief, and why.** Three checks that cost nothing and catch a real
  configuration: a non-positive duration and an integration with neither a
  binary nor a focus command. A third — `retention-visible` longer than
  `retention-known` — was removed the same day along with the duration it
  checked (D-26).
- **Dependency added**: `github.com/pelletier/go-toml/v2`, the only one so far,
  chosen over BurntSushi for its error messages — it points at the column and
  prints the surrounding lines, which is most of what "one logged complaint" is
  worth.
- **`doctor` is born here with the checks that exist** — paths, modes, socket
  lengths, configuration — and says in its own output what it does not yet
  check. M8 adds the session-watcher half.

### M3 — The record, the states, the reducer — **done 2026-09-17**

- **Delivered**, in the public root package because every one of these types
  crosses to an integration: `Kernel` with its ranks, `Event`, `Record` with
  round-tripping JSON, `Key` with a reversible filesystem-safe encoding,
  `Reduce`, `Apply`, and `CleanMessage`/`CleanLine`.
- **Done**: §A5.7's table is a test table — 27 rows, covering every event
  against a live session, an ended one and one that does not exist yet — and a
  record at schema 7 carrying two fields this build has never seen survives a
  read-modify-write with both intact. 46 tests across the module, vet clean.
- **`Apply` is where the reducer becomes useful**: pure, clock as a parameter,
  sharing nothing mutable with its input, so that M4's store can lock, read,
  apply and write without a state machine of its own.
- **Sanitising turned out to be a security surface, not tidiness.** Agent text
  leaves through status bars, tab titles and notifications, all of which are
  terminals or talk to one. An OSC sequence that survives the trip retitles a
  window; a CSI sequence moves the cursor and overwrites what is around it. Core
  is the only place that sees all of this text, so core strips escape sequences
  and control characters and repairs invalid UTF-8 (§A15). Newlines survive —
  flattening to one line is rendering, and belongs to whoever renders (R24).
- **What the plan got wrong, and now says correctly**: `ended_reason` (D-25),
  the pre-D-15 kernel names still in §A5.1's examples, and §A5.3's glyph table
  still written the way D-24 stopped writing it.
- **Raised here, answered the same day**: §A18 Q15, annotation provenance,
  closed by D-27. The record gained `derived_context` and `CapturedContext.By`
  as a result, and `Apply` gained one rule — replacing a captured context voids
  what was derived from it, in the same write.

### M4 — The session store — **done 2026-09-17**

- **Delivered** `internal/sessionstore`: atomic writes, the per-session `flock`
  with a deadline, the stamps the caller may not set, the move to `ended/` and
  back, `ForgetWhatIsTooOld`, and the per-session history file. Plus `agentnotify.History` as a
  public format, and `doctor` reporting what the store holds.
- **Done**: six writer *processes* each write twenty-five events to one session
  while a seventh reads without pause. The final sequence is exactly 150 — an
  exact proof of no lost update, not an approximate one — and the reader
  completed 6,447 reads with no unparseable record and no sequence going
  backwards. An ended session resurrects at sequence 5 with its original
  `created_at` and a cleared `ended_at`. 60 tests, stable across repeated runs.
- **`Update` is the primitive and `Apply` is a case of it.** A caller passes a
  function from the record on disk to what it should become; the store stamps
  `sequence`, `updated_at`, `schema` and `key` afterwards regardless of what the
  caller did to them. That is what `name` (M6) and `annotate` (M12) will use,
  and it is why naming a session cannot move `state_since`.
- **The measurement that changed the code** is in §A7.3: Go's `File.Sync` is
  `F_FULLFSYNC` on darwin, 36× the cost of the `fsync` the design asked for.
- **Deliberately not here**: liveness. A record says a process exists; whether
  it still does is M5, and `List` returning an ended-looking record for a dead
  agent is expected until then.

### M5 — Process facts on macOS — **done 2026-09-17**

- **Delivered** `internal/process`: `ProcessesOnThisMachine` (the sysctl
  reader), `Ancestry`, `FindAgent`, `BootIdentity`, `LivenessOf` and `Ended` —
  the last two pure functions of records and a `ReadsProcessFacts`, with `Ended`
  also taking the caller's own pid so it reads no global at all.
- **Done**: the decision table is ten rows against an invented machine —
  running, missing pid, reused pid, resolution-not-reuse, previous boot,
  unreadable machine, no process, no start time, no boot on either side. On this
  machine a killed and reaped `sleep` is judged gone, a record wearing this
  process's pid with the wrong start time is judged gone, and the walk from the
  test binary climbs `process.test ← go ← zsh ← claude ← nu ← zellij`, finding
  the real agent three rungs up. 75 tests.
- **Four corrections to what the design assumed**, all in §A8.2 and §A8.3 and
  logged as D-29. The boot identity one mattered most: `kern.boottime` drifts.
- **`doctor` grew a liveness line**, which reports the boot identity and how the
  machine judges each live session — looking only, since ending them belongs to
  the session-watcher.
- **Deliberately not here**: the `kqueue` watch. M5 is the facts; watching for
  an exit as it happens is the session-watcher's, in M8.

## B4. First light — it tracks real sessions

### M6 — record-agent-event, and seeing it — **done 2026-09-17**

- **Delivered** `agent-notify report-event`, `name` and `list`, and with them
  the whole hook path with no session-watcher anywhere: walk the ancestry, find
  the agent's process, capture what the configuration named, lock, reduce,
  write, exit 0, say nothing.
- **Done when**, and it is:

```
50  agent-notify  blocked-on-you/permission-prompt  12m  may I edit store.go?
40  thing         broke/rate-limited                 2m  429 from the API
30  docs          finished-a-turn                    5m  I rewrote the README.
20  build         working/running-tool               8s  running go test ./...
10  the-refactor  idle                              30m
```

  A pretend agent — a copy of the test binary, with a real `/bin/sh` between it
  and the hook — produces exactly that, `name` run from inside it finds the
  session it is in, and killing it turns the row into `ended/process-gone` on
  the next `list`. 80 tests.
- **R2 is tested rather than intended.** Six malformed invocations — no
  arguments, an unknown event, an empty session id, a bad flag, invalid
  `--agent-data` — each exit 0, write nothing to stdout *or* stderr, and leave
  an explanation in the log. An agent reads both streams and some exit codes.
- **Two things about macOS the test found**, both worth keeping: the kernel
  resolves a symlink before setting `p_comm`, so a symlinked `/bin/sh` reports
  `bash` and cannot be used to fake a process name; and macOS refuses to execute
  an unsigned copy of a system binary, while a copy of a Go binary runs fine
  because its ad-hoc signature covers the bytes that were copied.
- **Capture here is core's own** — process ancestry, and the variables a
  declarative container's capture list names, landing in
  `captured_context.by.<tool>` exactly as a coded capture would. Spawning an
  integration's `capture-environment` arrives in M12 (D-27).
- **Not here**: the datagram poke to a running session-watcher, because there is
  no session-watcher. The store carried the whole milestone on its own, which is
  what R4 says it must always be able to do.
- **The first visible milestone: your sessions in a terminal, with no daemon.**

### M7 — The claude agent-integration, and installing it — **done 2026-09-17**

- **Delivers** agent-notify-claude, its event mapping, and
  `agent-notify install claude` patching `settings.json` idempotently.
- **In** agent-integrations/agent-notify-claude. **After** M6.
- **Needs exploration**: the hook set and payload fields of the shipping Claude
  Code. The prior Rust adapter's mapping is the starting point — the one thing
  worth taking from it — re-verified rather than trusted.
- **[decided 2026-09-17 — D-31] `SessionStart` is three events, not one**, and
  splitting them on its `source` field is the adapter's job:
  - `startup` → `session-started`
  - `resume` → `session-started`, which resurrects whatever the store still
    remembers, keeping the session's age, name and sequence (§A5.7)
  - `compact` → `agent-progressed`, detail `compacting`
  Anything an agent fires that does not mean what the event name says is the
  adapter's to sort out. Core absorbing it would be core learning about Claude.
- **Done when** taking a turn in a real claude session moves its record through
  `working` into `finished-a-turn`, and a permission prompt shows
  `blocked-on-you`.
- **Delivered**: `agent-notify-claude`, its mapping, `install` with `--print`,
  and `agent-notify install claude` as a dispatcher to it (D-32).
- **Done**, and by more than the bar asked for. Real captured payloads driven
  through the adapter move a session `idle → working →
  blocked-on-you/permission-prompt → finished-a-turn`, and an idle nudge changes
  nothing. Beyond that, **7,980 real hook payloads from two days of ordinary
  use, across 29 sessions, replay through the mapping and the reducer** and
  produce clean `working ↔ finished-a-turn` alternation matching what a person
  actually did.
- **The exploration was the milestone.** Three mappings were decided by the
  replay and could not have been decided by reading documentation:
  - **A `SubagentStop` with no `agent_type` is not a subagent.** 413 of 416 real
    ones carry an empty kind and 245 of those fire within two seconds of a
    `Stop` — it is Claude's own turn wrapper. The first version of the mapping
    treated them as progress, and the replay showed it holding a finished turn
    at `working` for minutes while the human was the one being waited on. Only
    the three that named a kind are real subagents, and for those the mapping
    stands exactly as §A5.7 claims.
  - **`notification_type` discriminates reliably**, so the message text needs no
    parsing: all 64 real permission prompts carried `permission_prompt`, all 87
    idle nudges carried `idle_prompt`, and every permission prompt arrived
    directly after a `PreToolUse`.
  - **Resuming produces a decoy session.** A fresh id starts, lives about three
    seconds, and ends with reason `resume`, while the real session starts with
    source `resume`. Filing the decoy as `ended/superseded` is what keeps it out
    of a picker.
- **`StopFailure` is [assumed], not verified.** It did not fire once in two
  days, so its field names come from the prior Rust adapter. If they are wrong
  the event still arrives and the detail and message are simply empty.
- **A known gap, stated rather than hidden**: Claude has no interrupt hook, so a
  turn stopped with Esc leaves the session reading `working` until you next
  type. 50 of 363 real turns began and never saw a `Stop`. `idle_prompt` is the
  only signal available and mapping it to `turn-finished` would downgrade a
  pending permission prompt, which is worse.
- **Taken from the Rust adapter**, as permitted and all of it logic: the four
  notification kinds that want you, letting a notification with *no* kind
  through (that is what a permission prompt looked like before the field
  existed), and ignoring a kind nobody has heard of because the kinds that do
  not want you outnumber the kinds that do.
- **The first time it tracks something real.**

### M8 — The session-watcher — **done 2026-09-17**

- **Delivers** the `flock` singleton, detachment cutting all four inheritances,
  spawn-on-demand, reconcile-from-store at startup, `kqueue` exit watching, the
  sweep (prune, end what died, the boot-id rule), signals, the log,
  `watcher run --foreground`, and the first `doctor` output.
- **After** M6.
- **Done when** `kill -9` on an agent ends its record within milliseconds with
  no hook firing, a reboot leaves no ghosts, and a test proves a hook does not
  hang on an inherited pipe — the §A9.2 failure that would otherwise be found by
  a user rather than by us.
- **Delivered**: the `flock` singleton with its pid-and-version record, the
  detachment cutting all four inheritances, spawn-on-demand from the hook, the
  datagram poke, reconcile-from-store at startup, `kqueue` exit watching, the
  sweep, `SIGTERM`/`SIGHUP`, `watcher run|start|stop|restart|status`, and
  `doctor`'s watcher line.
- **Done**, with numbers. A `kill -9` on an agent — no hook can fire, that is
  what SIGKILL means — ends its record **4.67ms** later, with the sweep
  deliberately set 60 seconds away so that nothing but the exit watch can
  explain it. A record rewritten to carry a previous boot's identity is ended
  **4.48ms** after startup with its process still alive and never probed. And a
  hook handed a pipe, as Claude hands one, reaches end-of-file the moment it
  exits while the session-watcher it started keeps running — the failure that
  would otherwise have been found by a user.
- **The pokes carry `{key, sequence}` and the watcher ignores both.** It
  re-reads the store, because the store is the truth and re-reading is correct
  after any number of lost messages. A poke is a latency optimisation with no
  correctness attached (R4).
- **One behaviour visibly changed**: with a session-watcher running, a killed
  session is *filed* rather than merely rendered as ended, so it leaves the
  default `list` entirely (D-26). The M6 test was updated to say so.
- **A hazard M2 found, for whoever writes the detachment.** Paths are resolved
  from the environment, and §A9.2 sanitises the environment at spawn. Strip
  `TMPDIR` on macOS, or `XDG_RUNTIME_DIR`, `XDG_STATE_HOME`, `XDG_CONFIG_HOME`
  and `AGENT_NOTIFY_ROOT` on Linux, and the session-watcher resolves a
  *different* runtime directory than the clients that poke it. Nothing errors:
  the hook writes its record, the daemon watches an empty directory, and the bar
  stays blank. Either the sanitised environment carries those forward, or the
  spawner passes the resolved layout explicitly — the second is safer, since it
  cannot drift with the environment.

## B5. The product

### M9 — The socket layer and the SDK — **done 2026-09-18**

- **Delivered** the wire protocol in the public package, the stream listener and
  handshake with its permanent version refusal, the in-memory snapshot on
  connect, coalesced deltas, bounded queues with resync on overflow, `wake_on`
  filtering; the `subscribe` package with its lifecycle entry point,
  reconnection and cold store reader; the fake session-watcher; and
  `agent-notify tail`, `record` and `replay`.
- **Done**, all three parts. A ten-line subscriber sees changes live — this is
  `agent-notify tail` against a real agent-integration, unedited:

```
── 22:11:49: 1 session(s) ──
20  agent-notify  working  0s   show me the socket layer
22:11:49  50  blocked-on-you/permission-prompt  Claude needs your permission to use Bash
22:11:50  30  finished-a-turn                   a ten-line subscriber, live
```

  It survives a session-watcher going away entirely and a new one arriving,
  without being told and without asking. And a subscriber that stops reading
  while 400 sessions change has its queue thrown away, is sent one snapshot,
  and ends up holding all 400 with the one that changed *after* the overflow
  showing the state that was true when it caught up — a full redraw, never a
  wrong render.
- **The public surface is now three packages**, which is the layering M7 forced
  followed through: `agentnotify` the vocabulary and the formats, depending on
  nothing; `agentnotify/hook` for an agent-integration; `agentnotify/subscribe`
  for a tool-integration.
- **`Differs` compares the encoded record, not the struct**, so that a field
  added to `Record` is compared automatically and the fan-out filter cannot
  quietly fall behind the thing it filters.
- **Not here**: the enricher invitation and container coordinates (M12), and
  supervising the integrations the session-watcher starts (M13). Unsolicited
  connections — a subscriber nobody spawned — are first class and already work,
  which is what `tail` is.

### M10 — zellij, the display half — **done 2026-09-18**

- **Delivered** agent-notify-zellij: pane and tab renaming addressed by id, the
  tab aggregate as `max(urgency-rank)`, giving a pane back when its session
  ends, the name fallback, glyph configuration with a `kernel/detail` table,
  and `install` / `repaint`. In core: `Glyphs` and `MostUrgent`,
  `Integration.Settings` and `ConfigFile` in the SDK, and the `WantEnded` fix of
  D-35.
- **Done.** The whole chain, with only the agent replaced by a script — a real
  detached zellij, real hooks through the real store, the real session-watcher,
  the real socket, the real display (glyphs shown as ASCII here; they ship as
  Nerd Font marks):

```
both working                        alpha wants an answer
  pane 4   ↻ agent-notify             pane 4   ▲ agent-notify
  pane 5   ↻ lenny                    pane 5   ↻ lenny
  tab      ↻ notes                    tab      ▲ notes

beta finished its turn              alpha's agent exited
  pane 4   ▲ agent-notify             pane 4   shell-a
  pane 5   ▢ lenny                    pane 5   ▢ lenny
  tab      ▲ notes                    tab      ▢ notes
```

  The last frame is the one worth looking at: the pane went back to the title
  zellij gives it, and the tab followed the aggregate down rather than keeping
  the glyph of an agent that is gone.
- **What zellij's CLI turned out to allow** (0.45.1): `rename-pane --pane-id`
  and `rename-tab-by-id` address a pane or tab that is not focused, which is
  what makes an external display possible at all — without them you can only
  rename what you are already looking at. `list-panes --json --tab` answers
  every question in one call: pane ids, titles, which tab each is in, and that
  tab's name. See D-35 for the two facts that shaped the code.
- **Which pane an agent is in comes from the declarative capture list**
  (`capture = ["ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"]`), which already
  existed. M12 replaces it with this integration's own `capture-environment`
  without changing anything here: the record stores an opaque blob either way,
  and only who produced it changes.
- **Not here**: focus, coordinates and `interpret-environment` (M12), and
  nothing starting this for you (M13) — until then it is an unsolicited
  connection, which is first class and is what M9 built.
- **[renamed 2026-09-18 — D-37]** `agent-notify-zellij` became
  `agent-notify-zellij-display`, and the name it hands over in the handshake and
  answers to in the config went from `zellij` to `zellij-display`. The container
  is a separate repository, installed separately.

### M11 — The second agent: codex — **done 2026-09-18**

- **Delivered** agent-notify-codex — seven of codex's twelve hooks, the
  append-only install into `~/.codex/config.toml`, and a replay of the 90 real
  payloads this machine recorded — plus the ninth event in core (D-36).
- **Done.** Two agents, two entirely different hook shapes, one vocabulary and
  one bar. Claude's permission prompt is a `Notification` carrying a
  `notification_type`; codex's is a `PermissionRequest` carrying a `tool_input`.
  They arrive as the same state:

```
  agent   session        state                             pane
  claude  agent-notify   working                           ↻ agent-notify
  codex   lenny          working                           ↻ lenny
                                                           ↻ notes   <- the tab

  codex   lenny          blocked-on-you/permission-prompt  ▲ lenny
  claude  agent-notify   blocked-on-you/permission-prompt  ▲ agent-notify
                                                           ▲ notes

  claude  agent-notify   blocked-on-you/permission-prompt  ▲ agent-notify
  codex   lenny          idle/interrupted                  lenny
                                                           ▲ notes
```

  The last frame is codex-specific and shows why the ninth event exists: Esc on
  the codex session drops it to `idle`, its pane gives the glyph back, and the
  tab keeps claude's — the aggregate following the most urgent thing left.
- **The vocabulary was short by exactly one**, which is the answer to the
  question this milestone was placed to ask. Everything else mapped without
  argument: nothing about codex reached core, no kernel changed, the reducer
  kept its shape, and `blocked-on-you/permission-prompt` means the same thing
  for both agents without either adapter knowing the other exists.
- **What codex forced that claude did not**: the event arrives *in* the payload,
  so one command string serves every subscription; `PermissionRequest` is
  **synchronous** and reads a handler's stdout as a verdict on the tool, so R2's
  "never write to stdout" stops being hygiene and becomes the difference between
  notifying and silently approving a command; and codex asks a person to trust a
  hook, which the installer deliberately does not answer on their behalf.
- **`PreToolUse` is not subscribed**, for a reason worth keeping: it fires 49ms
  before `PermissionRequest`, which is well inside the window where two
  short-lived processes can reach the session lock out of order. The one that
  arrives second wins, and losing that race paints "working" over a permission
  prompt.
- **Not here**: nothing. The third agent is not in this plan (§B8), and the
  event list is expected to hold.

### M12 — Containers, coordinates, and focus — **done 2026-09-18**

- **Delivers** the container capability, both capture forms — the declarative
  capture list and spawning an integration's `capture-environment` under
  `capture-timeout` (D-27) — `interpret-environment` over the socket, ordered
  focus with typed failures, the three-valued queries, **agent-notify-zellij-
  container as its own repository** (D-37), `focus-session`, and `annotate`.
- **After** M10.
- **Done**, all four. Against a real attached zellij, in another tab:

```
$ agent-notify focus-session m12          active tab: 1 -> 3
  zellij-container       focused
agent-notify is in front.                 exit 0

$ agent-notify focus-session m12          clicked twice, still 0
$ agent-notify focused m12                yes                        exit 0
$ agent-notify focused m12                no                         exit 1
    zellij-container: tab 3 is not the active one

$ agent-notify focus-session solo         with nothing configured:
cannot focus tmp: no-container-configured — nothing is listed in [container] order
Nothing is configured to place a session. Everything else works without one:
states, counts, liveness and history need no container at all.
```

  A resume re-derives rather than taking you to the wrong pane: a new agent
  process makes the hook capture again, the write that replaces the captured
  context voids the coordinates in the same atomic write, and the session-watcher
  places it again — `pane 60, tab 3` became `pane 17, tab 1` with nothing in
  between ever pointing at somebody else's pane. And a `capture-environment`
  that never returns cost a hook **1.5 seconds** against a 1-second
  `capture-timeout`, contributed nothing, and cost the event nothing at all.
- **What it taught** is in D-38, D-39 and D-40.

### M13 — Supervision — **done 2026-09-18**

- **Delivered** the reconciler in the sweep, the consecutive-failure count and
  what resets it, the handshake as the health check, giving up,
  reload-as-retry, a child's stderr relayed into the log, and `doctor`'s
  per-integration report.
- **Done**, both halves, against the real zellij display rather than an imagined
  child. Nothing was started by hand:

```
integrations ok    reported by pid 49566 at 23:37:44Z
             zellij-display         connected, pid 49569 [display]
    pane 4: ~ agent-notify          … and it was painting

$ rm /tmp/an-bin/agent-notify-zellij-display
integrations fail  reported by pid 49566 at 23:38:06Z
             zellij-display         gave up
                                    zellij-display failed 3 times; last: cannot start
                                    /tmp/an-bin/agent-notify-zellij-display: no such file or directory
                                    fix it and run `agent-notify watcher reload`

$ go build -o /tmp/an-bin/agent-notify-zellij-display .      (nothing happens)
$ agent-notify watcher reload
integrations ok    reported by pid 49566 at 23:38:19Z
             zellij-display         connected, pid 50974 [display]
    pane 4: O agent-notify          … and it is painting again
```

  The session-watcher is pid 49566 throughout. Reinstalling alone changed
  nothing, which is the point: giving up is a decision, not a pause.
- **Supervised or not is derived, never configured** (D-41). An integration is
  supervised if it is enabled, has a binary, and is *not* named in
  `[container] order` — because a container is a program that gets run when
  something needs an answer, not a daemon that stays connected (D-38). No new
  key, and nothing that can disagree with what an integration declares.
- **`doctor`'s report is a file, not a socket message**, and that is a
  diagnostic decision: doctor's hardest job is a session-watcher that holds the
  lock and does not answer (§A9.2), and a report you have to ask for over the
  socket is exactly the report you cannot get then. It is also readable with
  `cat`.
- **Q17 is answered**: a supervised child's stderr is relayed into
  agent-notify's log, line by line, tagged with its name. A child started by a
  daemon has no terminal to complain to, and a display whose failure is
  invisible is a display nobody can fix.
- **The log records state changes, not attempts**: three warnings and one error,
  then silence. A supervisor that spams is a supervisor nobody reads.

## B6. Breadth

### M14 — sketchybar — **done 2026-09-18**

- **Delivered** agent-notify-sketchybar: a counter per state with its count, a
  chip behind each listing the sessions in it with their ages, click-to-focus on
  both the chip rows and the counter itself, and a repaint clock so the ages
  stay true. In core: `Palette` (what `Glyphs` became, because the same
  resolution serves colours), `Record.Elapsed` and `Ago`, and
  `Integration.CoreBinary`.
- **Done.** On the real bar, started by the session-watcher and never by hand:

```
  A blocked-on-you  on   1    A agent-notify  permission-pr…  now
  X broke           off  0
  O finished-a-turn on   1    O lenny                         now
  ~ working         on   2    ~ agent-notify   2m | ~ nushell  1s
  . idle            off  0
```

  A state with nothing in it keeps its place and is not drawn, because a bar
  whose items move about as the day goes on is a bar nobody can click. And when
  the display stopped, it took its items off: a counter left behind says "two
  agents are working" for ever and there is no way to tell it from a true one.
- **What sketchybar's model turned out to allow** is in D-43. The short version
  is that everything is forgiving, which is what lets one render be one
  idempotent invocation naming every item it owns — and that the exit code
  reports only the last message in the batch, so the complaints are the truth.
- **[2026-09-18, after a day of using it]** the counters were not enough: a bar
  is read when it changes, and a number going from 1 to 2 does not change
  enough. It now announces — the counter lit, the chip opened on the agent's own
  words, the row lit inside it, and all of it back eight seconds later — and a
  hover on a row shows what that agent last said, scrollable. D-47 and D-48.
- **It is the first display that needed a clock.** A chip says how long a
  session has been waiting, and nothing will ever announce that four minutes
  became five. R26 says that is rendering rather than state, and M14 is where
  the rule earned its keep: storing the age would be a write per second per
  session, each one waking every display to say nothing.

### M15 — aerospace, and nested focus — **done 2026-09-18**

- **Delivered** agent-notify-aerospace-container: the outermost rung of a focus.
  In core: `container.Ambiguous`, a focus walk that steps past a container which
  says it has nothing for this session, and a declarative container's unfilled
  placeholder promoted from "nothing to do" to a refusal (D-45).
- **The exploration's answer is that the thing this milestone was named after
  does not exist.** There is no walk from an agent to its window. Three
  measurements, each one on its own enough:

  - Nothing in a shell's environment names a window, and Ghostty's CLI will not
    say either — `ghostty +new-window` answers "not supported on this platform".
  - A pid names an application, not a window: aerospace reports an `app-pid` per
    window and two windows of one application carry the same one. Measured with
    two TextEdit windows, one process, one pid under both, because that is a
    thing that can be opened and closed safely.
  - Inside a multiplexer the process chain does not even reach the terminal. An
    agent's ancestry ends at the zellij SERVER, whose parent is 1; the client
    drawing it is in a different tree entirely. This is the ordinary case, not a
    corner of one — it is what every session on this machine looks like.

  So the window is **found** rather than known, from two keys that are weak
  separately and good together: the process chain, which narrows to one
  application and is exact when that application owns one window, and a
  window-title prefix built from a configurable template, which is the only
  index that survives a multiplexer.
- **Done.** From Firefox, on another workspace, one command:

```
looking at:  window 107 Firefox, workspace 2
$ agent-notify focus-session m15-demo
  aerospace-container    focused
  zellij-container       focused
agent-notify is in front.
after:       window 34 Ghostty, workspace 1
focused?     yes
```

  The coordinates behind it, derived by the session-watcher from what the hook
  captured, are a chain ending at pid 3655 (`zellij`, the server) and
  `"title": "home | "` for aerospace, and `{session: home, pane: 17, tab: 1}`
  for zellij. Neither container knows the other exists.
- **What it changed in the design** is D-44: a window is not a property of a
  session, so nothing about one is stored and the lookup lives at the moment of
  use. That is the first container whose interpretation asks its tool nothing,
  and §A11.4's prediction about why aerospace would need a binary was wrong in
  an interesting way — it needs one for its *capture*, which walks a process
  chain, and not for its interpretation at all.
- **What it cost** is written into a test rather than a footnote: inside a
  multiplexer there is no chain to narrow by, so a browser window whose page
  title starts `home | ` matches the key and is believed. The separator is what
  keeps that survivable, and the alternative — refusing to focus anything inside
  a multiplexer — is worse.

### The switchover — **done 2026-09-18**

Not a milestone, an event: this is now the agent-notify running on the machine
it was written on, and the nushell implementation it replaces is off. Ten Claude
hooks, both zellij halves, the menu bar and aerospace, with `[agent.claude]
binary = "claude"` so that liveness has a process to judge. What went is the
nushell hook block, its sketchybar block and its prune-daemon launchd job; what
stayed is the raw-event recording rig, which is how every payload question so
far has been answered.

**Three things only a real machine could have said**, all three now fixed and
tested:

- **A display connects after your config file has run**, so everything it owns
  lands at the far end of whichever side of the bar it is on. Somebody whose bar
  already has a layout needs to be able to say *where* — hence `before` and
  `after`, naming an item they made themselves.
- **A popup with no background is transparent.** sketchybar's default is
  `background.drawing=off`, so the chips were rendered perfectly and were
  unreadable over a terminal full of text. A display that owns a popup has to
  paint it.
- **A reload has to restart what it reloaded** (D-46).

What is knowingly missing is the picker: M16. Its zellij keybinding still points
at the nushell one, which now opens on an empty store.

### M15a — The macOS menu bar — **done 2026-09-18**

- **Split into two repositories on the day it was finished** (D-54):
  `agent-notify-macos-bar` and `agent-notify-macos-notifications`. Everything
  below was built as one program and is true of the pair.
- **Delivers** `agent-notify-macos-bar`: the semaphore in the real menu bar,
  with no sketchybar and no bar of anybody else's. One `NSStatusItem` whose
  title is the counters and whose menu is every live session, choose one to
  focus it.
- **Why it is not a port of M14.** sketchybar has five counters because a bar
  cannot hold a menu, and a chip behind each of them because a bar cannot hold a
  list. A menu bar can hold both, so the native shape is one item and one menu:
  the states are its sections, the sessions are its rows, and the agent's last
  message is the row's own tooltip. Everything M14 had to invent — a scroll
  register kept in an item's icon, a wheel forwarded through a custom event,
  forty lines written so that six can be drawn, agent text quoted into `sh` —
  has no counterpart here, because a menu scrolls by itself and a menu item
  takes its text as argv.
- **What it costs is the first cgo in this project**, and whether that is
  affordable was measured rather than assumed [verified 2026-09-18, macOS
  26.6.2, arm64]:
  - **A bundle-less Go binary really can own a menu bar item.** `NSStatusItem`
    windows are hosted out of process and never appear in
    `CGWindowListCopyWindowInfo`, which is a trap: the naive check says the item
    is not there when it is. The way to ask is the accessibility API —
    `AXExtrasMenuBar` on our own pid lists the item by title — and that is also
    the only test oracle there is for this display.
  - **A `.app` bundle is needed for exactly one thing, and it is not this.**
    `UNUserNotificationCenter` does not return an error without a bundle
    identifier, it aborts the process: `bundleProxyForCurrentProcess is nil`. So
    native notifications are a separate question from a status item, and not one
    this milestone answers.
  - **The toolchain is load-bearing.** Go 1.21 builds a cgo binary whose ad-hoc
    signature is invalid — `codesign -v` says "code or signature have been
    modified" — and macOS 26 SIGKILLs it on launch, silently, before `main`.
    Go 1.26, which every module here already declares, signs it correctly. There
    is therefore no codesign step in the install, and there is a minimum Go.
- **Done when** the item is on the bar with the right counts, its menu lists the
  live sessions by urgency, choosing one focuses it through the same
  `focus-session` every other display uses, and a state change says so without
  anybody clicking anything.
- **Done.** The live display, and then the accessibility API asked what the menu
  bar is really showing and what is inside it:

```
$ agent-notify list
30  dmilog3-rollout-dashboa…  finished-a-turn  3m   Fatto, tutte e tre…
20  agent-notify-go-design    working          20m  let's work on the default macos display
20  tmp                       working          3m   andiamo

$ ax AXExtrasMenuBar (pid 91889)
  menu bar extras: 1
    [0] ●1 ◐2
  AXPress ->
    finished a turn                      AXMenuItem
    ● dmilog3-rollout-dashboard    3m    AXMenuItem
    working                              AXMenuItem
    ◐ agent-notify-go-design      20m    AXMenuItem
    ◐ dashboard                    3m    AXMenuItem
```

- **What it changed in the design** is D-49 through D-52: the boundary to a UI
  toolkit is a document rather than an API; an announcement may take an item but
  never the keyboard, which leaves this display with no edges at all; what a
  paint costs is what decides which fields are worth waking for; and a macOS
  display has to be a bundle, because a bare binary cannot remember where its
  item was put and the place it goes back to is not drawn.
- **The icons are the system's own.** Geometric characters from a font were what
  the first version drew, and beside Bluetooth and the battery they read as text
  somebody had typed into a menu bar rather than as part of one: a font glyph is
  a letter that happens to be round, where an SF Symbol is rendered at the bar's
  optical size and weight and hinted for it. In the menu they go in each item's
  image well, which is where AppKit draws an icon. A symbol this macOS does not
  have draws NOTHING — not a box, not a fallback — so the names are validated
  when the config is read, the way colours and fonts already were, and every
  state keeps a text glyph behind its symbol.
- **The item took three designs, and the third one is not a count at all.**
  Five counts, one per state, was noisy. One count for the most urgent state was
  worse: the single symbol changes identity whenever the agents do, so a glance
  has to be decoded before it can be read, and eight agents working with one
  waiting looks exactly like one agent waiting. Back to five counts, and then
  the question that settles it — a bar item is asked ONE thing, *do I need to do
  something*, and a row of numbers is not an answer to it. A single mark answered
  it and threw everything else away. The fourth is the marks without the
  numbers: **one alien per state that has anybody in it**, in that state's own
  colour, and the ones waiting on a person FLICKER while the ones getting on
  with it sit still. The shape never changes, so the bar is read by colour and
  by movement rather than by being parsed; how many are in each state moved to
  the tooltip, which is also what the item is called to the accessibility API,
  because a picture has no name of its own. **What counts as wanting you is
  core's rule** — rank above working — the same predicate that earns a
  notification, so the bar and the banner cannot disagree.
- **Colour and movement are two budgets, and only one of them was being spent.**
  The palette rule here was "colour only where it means something", which is
  right for an item that shows ONE mark and wrong for one that shows several:
  five identical sprites side by side are told apart by hue and by nothing else.
  So every state now has its own Rosé Pine hue and what is rationed instead is
  movement — the thing that actually interrupts somebody. The general form: a
  channel is only worth rationing while it is the channel carrying the signal.
  The hues are then chosen against each other rather than one at a time, and
  what has to be distinguishable is anything in the same movement class: two
  blues side by side on a translucent bar are one blue.
- **The menu wears the same mark as the bar, and never moves.** Five different
  SF Symbols in a menu read as five unrelated things stacked up; the same sprite
  in five colours reads as one program. And the flicker stops at the bar on
  purpose — it is a signal for something somebody is trying to catch out of the
  corner of an eye, and a menu is already being looked at, where a blinking row
  is just a row that is hard to read.
- **`NSStatusItem.button.alphaValue` does nothing** [measured 2026-09-18, macOS
  26.6.2]. It takes the value, reads it back, and the item on the screen does
  not change — because the item's window is hosted by another process and what
  crosses the boundary is drawn content. A colour is content; an alpha on a view
  is not. The flicker therefore redraws the title at a lower opacity on a timer,
  which is also the cheaper design: the paint says WHETHER to flicker and the
  applier decides what that looks like, so nothing is sent twice a second for as
  long as somebody leaves an agent waiting.
- **The palette is the machine's, not the system's.** Rosé Pine, the theme
  everything else on this machine is themed in, with two hues and no more: Love
  for `blocked-on-you`, Gold for `broke`, and the palette's text colour for the
  three that do not want anything — told apart by shape, which is what the
  shapes were chosen for. The one thing that does not survive the move onto a
  menu bar is a palette's DIM tones: Subtle and Muted are built for an opaque
  `#191724`, and a translucent bar is lighter than that, so on it they come out
  darker than the system's own secondary label. `idle` is therefore the text
  colour at 80% rather than a second grey. What it costs is light mode, which is
  a fixed palette's price everywhere and is one line of config to undo. The app icon is in
  the palette too — a plum tile with an Iris space invader on it — and
  deliberately in neither state hue, because an icon in Love would read as an
  agent waiting for you every time a banner appeared.
- **A PNG written straight out of AppKit is not in the colours you asked for**
  [measured 2026-09-18]. Drawing happens in Apple's calibrated RGB, so
  `#c4a7e7` lands on disk as `#b693e1` with a Generic RGB profile beside it:
  correct on a colour-managed Mac, and wrong to everything that reads pixels —
  including the test that checks the icon is drawn in the palette.
  `bitmapImageRepByConvertingToColorSpace:` to sRGB before writing makes the
  file say what it means. The general form is the same as the knockout one: the
  picture you asked for and the picture that comes out are two different
  questions, and only one of them is testable.
- **A palette colour fills the knockout** [measured 2026-09-18, macOS 26.6.2].
  `NSImageSymbolConfiguration configurationWithPaletteColors:` on a `.fill`
  symbol does not tint it, it floods it: the middle scanline of a rasterised
  `questionmark.circle.fill` goes from `..#####..#####..` to `..############..`
  and the question mark is gone. Every filled symbol this display drew arrived
  on the bar as a coloured blob. Painting the hue over the drawn symbol with
  `NSCompositingOperationSourceAtop` leaves the destination alpha alone and the
  hole stays a hole. The general form: **a tint that goes through the symbol API
  is a different picture from a tint that goes over the pixels**, and only one
  of them is the icon you chose.
- **It was shipped before it was visible**, which is the milestone's own lesson.
  Every test passed, the accessibility API confirmed the item and its menu, and
  the item was not on the screen — because the tests' oracle can see an item the
  menu bar has decided not to draw. D-52 has the measurements; the habit worth
  keeping is that for anything with a surface, the last check is a screenshot.
- **What it cost, against M14, is nothing** — and that is the surprise worth
  recording. The chip, the scroll register in an item's icon, the wheel
  forwarded through a custom event, the forty written lines of which six are
  drawn, the quoting of an agent's words into `sh`, the structure sent
  separately from the values, the self-repair when a bar reload loses an item:
  none of it has an equivalent here, because a menu scrolls itself, a menu item
  takes its text as argv, and nothing this display owns can be taken away from
  it by somebody reloading a config. M14's render is 440 lines of code; this
  one is 127, plus 329 lines of Objective-C that decide nothing.

### M15b — Tokens — **done 2026-09-20**

- **Delivers** `usage` on every record: four disjoint counters, the reasoning
  subset, and the cursor that makes a second read of the same tail cost nothing
  (§A7.4.3, D-61). Both adapters fill it on the hooks they were already firing,
  so no hook was added and no write was. *[amended 2026-09-22 — D-76] It
  delivered the context level and its limit too, and those are gone: only one of
  the two agents could report a limit, so only one ever got a percentage.*
- **Why core does the arithmetic.** The adapter is a one-shot process and the
  running total is in the record it has not written; the only alternative is
  re-reading the whole transcript per hook, which grows with the session
  forever. Responses in, totals out — and the split lands the agent-specific
  half in the adapter and the previous value in core, which is where each of
  them already was.
- **The measurement that decided the shape.** Claude repeats one response's
  `usage` on every content-block line of its transcript: 3401 assistant lines,
  1869 responses, a 2.11× overcount on output for anyone who sums lines. codex
  keeps its own cumulative and needed none of this, and is fed through the same
  path anyway so that core has one rule rather than one per agent.
- **Done when** a bar can show what a session has spent without knowing which
  agent it is, and a fifty-tool-call turn still renders once.

### M15c — What the agent knows — **done 2026-09-20**

- **Delivers** `model` and `branch` on every record (§A7.4.4, D-65), on the same
  best-effort terms as M15b: reported on hooks that were firing anyway, empty
  when the answer is not there, and never a reason for a hook to do more work
  than it was already doing. *[amended 2026-09-22 — D-76] It delivered `quota`
  as well, which only codex ever filled and which went for that reason.*
- **What it cost each side.** Codex: one field off the payload, and — until
  D-76 — the rate-limit half of a line already being read. Claude: one field off the
  transcript line already being read, because its payload has no model and never
  did. Core: `internal/branch`, forty lines that read `.git/HEAD` and follow a
  worktree's `gitdir:`.
- **The measurement that decided who reads the branch.** Claude writes
  `gitBranch: "HEAD"` for a repository on `main` when the session's directory
  merely contains checkouts, and codex writes the branch once at session start.
  Neither is wrong-but-usable; both are confidently wrong, which §A11.3 rates
  worse than absent — so core reads it.
- **Done when** a bar can group four sessions by model and say which is on which
  branch, without knowing which agent any of them is.

### M16 — The picker

- **Done when** a floating pane lists sessions by urgency and jumps to the one
  you choose.

### M17 — Linux — **done 2026-10-09**

- **Delivers** `pidfd_open` exit watching and `/proc` start times, verified the
  way the macOS equivalents were on 2026-09-16.
- **Done when** the M5 and M8 tests pass on Linux, and the two [assumed] marks
  in §A8 become [verified].
- **Delivered**: a `_linux.go` beside each of the three `_darwin.go` files and
  nothing else — `process` reads `/proc`, `sessionwatcher` watches exits with
  pidfds in one `epoll` set, and `storewatch` watches the store with inotify.
  The last was not in the milestone as written: it arrived with D-83, after
  M17 was.
- **Done**, in Docker Desktop's VM (Linux 6.10.11, arm64), as a user who is not
  root: every test in core and in the claude, codex, picker and zellij
  integrations passes. A `kill -9` on an agent is filed 0.7ms later with the
  sweep five seconds away, and a record from a previous boot is ended 0.66ms
  after startup with its process alive.
- **The start time is not a date on Linux** (D-89): the conversion through
  `btime` moves when the clock is set.
- **Three tests were right on macOS by timing alone**, and Linux, which starts
  a process faster, failed them. `list --all` read `sessions/` and then
  `ended/`, so a record filed between the two was listed twice; core now keeps
  the higher sequence of the two, which is how `Read` settles them. The test of
  a fast exit waited for a kernel of `ended`, which `list` shows for a dead
  process with no session-watcher at all; it now waits for the record to be
  filed. And the lock test retried so tightly that the holder it spawned could
  not get in, one run in two; it now lets go for 10ms between tries.
- **Not done here**: running a real agent on a Linux desktop. Nothing in the
  agent-integrations is platform-specific, but nobody has watched one.

## B7. Distribution

### M18 — Installable by somebody else

- **Delivers** a README per repository, versioning and releases, `wait`,
  `history`, `doctor` completeness, and the guide to writing an integration.
- **Done when** somebody who is not us installs core plus one integration from
  published artifacts and ends up with glyphs on their bar.

## B8. Not in this plan

Windows, remote sessions, a global event log and writing back into a session are
not missing steps. They are decisions recorded in §A1, §A7.7 and §A10.3 that
would have to be reversed first.
