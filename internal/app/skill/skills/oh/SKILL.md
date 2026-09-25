---
name: oh
description: oh's own configuration, capabilities, sandbox grants, skill layout, and stored sessions. Use when editing oh.toml or oh/config.toml, granting an agent read/write/shell/network, adding a sandbox path or a skill, working out why oh refused a file or command, reading what earlier sessions did, or if working on the oh harness itself.
---

# oh

oh is a terminal agent. One session is one conversation, its tools gated by capabilities, its reads and writes confined to granted paths, its journal stored and replayable.

This skill ships inside the binary and describes the version running.

## Configuration

Three sources load in order, each overriding the last:

| Layer     | Path                                          | Scope                    |
|-----------|-----------------------------------------------|--------------------------|
| Defaults  | `internal/app/config/defaults.toml`, embedded | Every session            |
| Global    | `~/.config/org.crdx/oh/config.toml`           | Every session            |
| Workspace | `<workspace>/oh.toml`                         | Sessions in that project |

Global and workspace files are watched and auto-reload. A reload applies live to the theme, bar, snippets, permissions, streaming, editor command, and tool output cap. Everything else waits, and waits for one of two things:

| Setting                                           | Lands               |
|---------------------------------------------------|---------------------|
| `[sandbox]`, `[skills]`, `[provider]`, `[ports]`  | when oh next starts |
| `caps.default`, `[model]`, `[tools]`, the toolbox | in a new session    |

The first group belongs to the process, so a restart that resumes this very conversation picks it up. The second is frozen into the session when it is first created, and a resumed conversation restores what was frozen, so only a new session reads it afresh.

A sandbox grant added mid-session is therefore neither refused nor half-applied: this process never reads it. Nor is one removed mid-session revoked. Say "when oh next starts", not "in a new session", because the two are not the same thing.

A key nothing reads, including an option a bar segment does not know, is ignored and named in a warning at startup rather than being fatal. A key that is read but holds something invalid does stop startup.

Where `~/.config/org.crdx/oh/` is a symlink into a dotfiles repository, edit the target.

## Global Config

`~/.config/org.crdx/oh/config.toml` holds what follows the user between projects, and needs a `version` key. Beside it sit `SYSTEM.md`, prepended to every session, and `snippets/` for snippet bodies kept in files.

- `[model]` — `round_robin` of `provider/model@effort` entries to rotate through; `effort` and `fast` defaults
- `[provider.ollama]` — `host` for a local or LAN endpoint
- `[snippets]` — `/name` expansions, inline or `{ file = "snippets/name.md" }`; `{{ .Arg }}` takes the rest of the line
- `[ui]` — `streaming`, `grouping`, `reasoning`, `currency`, `[ui.theme]`
- `[permissions]` — `ask` or `allow` per gated action, deciding what a granted capability buys
- `[tools]` — custom tools, each `[tools.<name>]` naming a `command` to run and the parameters the model supplies
- `[caps]`, `[sandbox]`, `[skills]` — defaults each workspace then overrides
- `[bar.top]`, `[bar.bottom]` — status bar segments, each naming a `segment` and its options
- `[editor]`, `[input]`, `[tool]`, `[ports]` — editor command, continue behaviour, tool output cap, exposed-port hostname

## Workspace Config

`<workspace>/oh.toml` covers sessions in that project alone. Omit `version`. Set only the keys that differ.

Scalars replace. An array replaces rather than merges, so overriding one bar position means restating every segment in it.

A workspace file comes with whatever repository you cloned, so it may not set what decides where the agent may reach or what runs on your machine. Setting one of these fails at startup, naming the file and the setting:

- `[editor]`, whose `command` runs on your machine
- `[sandbox]`, which says where the agent may read, write, and execute
- `[skills]`, which says what instructions reach the model
- `[provider]`, which says which endpoint the conversation goes to
- `[tools]`, whose commands run on your machine
- `[experimental]`, which is unknown by definition

Those belong to the config that follows you between projects, and paths in them resolve relative to it, with `~` expanded. The table is `[skills]`, plural.

Text a workspace file supplies is refused wherever the terminal would obey it rather than draw it. A bar segment's options hold no control characters, and `ports.hostname` holds only letters, digits, dots, dashes, and its one `{session}`.

## Theme

Every `[ui.theme]` value is a space-separated list of one colour and any number of decorations. The colour is `#rrggbb`, or `"default"` for the terminal's own. Anything else fails at startup, as does a second colour.

The decorations are `bold`, `faint`, `italic`, `underline`, `blink`, `reverse`, `hidden`, `strikethrough`, and `overline`. An underline also takes a shape as `underline:single`, `underline:double`, `underline:curly`, `underline:dotted`, or `underline:dashed`, and a colour of its own as `underline:#rrggbb`. Order is free; the terminal decides what it can draw.

| Key               | Paints                                           | Default   |
|-------------------|--------------------------------------------------|-----------|
| `normal`          | answers, tool calls, typed input, plain syntax   | `default` |
| `dim`             | reasoning, tool results, rules, quotes, comments | `#969896` |
| `accent`          | prompt, spinner, subject, inline code, bullets   | `#c08050` |
| `user`            | the background behind a user message             | `#343541` |
| `harness`         | the background behind a harness message          | `#303a43` |
| `status_success`  | reads, inserted lines, a cheap turn              | `#4c9a2c` |
| `status_info`     | shell, network, git, links, diff hunks           | `#81a2be` |
| `status_warning`  | writes, headings, changes, a stopped turn        | `#cfad00` |
| `status_danger`   | failures, hazards, deleted lines                 | `#cc6666` |
| `syntax_type`     | types in highlighted code                        | `#f0c674` |
| `syntax_literal`  | literals in highlighted code                     | `#b5bd68` |
| `syntax_operator` | operators in highlighted code                    | `#8abeb7` |
| `syntax_keyword`  | keywords in highlighted code                     | `#c9a6d4` |
| `skill`           | skill names in a tool call                       | `#c9a6d4` |

`user` and `harness` paint backgrounds; keep both near the darkness of their defaults. Every other key is a foreground. A decoration reaches every role the key paints, so `dim = "#969896 italic"` italicises tool results as well as reasoning.

```toml
[ui.theme]
status_danger = "#cc6666 bold"
status_warning = "#cfad00 underline:curly underline:#cc6666"
dim = "default faint"
```

Some styling is fixed in the binary — the italics on reasoning and preview hints, the bold on a heading, the simulation gradient — and no theme key reaches it. `internal/app/style/theme.go` and `style.go` are canonical.

## Bar

The bar is the pair of rules drawn above and below the input block. `[bar.top]` and `[bar.bottom]` each take `left`, `center`, and `right`, an array of tables where every entry names a `segment` and sets that segment's own options.

```toml
[bar.bottom]
left = [
    { segment = "mode-toggle" },
    { segment = "workspace-dir", type = "short" },
    { segment = "git-branch", rate = "10s" },
]
right = [
    { segment = "local-time", format = "15:04:05" },
    { segment = "session-name", emoji = true },
    { segment = "scroll-overflow", direction = "down" },
]
```

A segment drawing nothing is left out, and ` ─ ` joins whatever is left. Each of the six positions is independent, a segment may appear in several, and an array replaces rather than merges, so restate the default entries of any position you override.

The right side is drawn whole and the left is then fitted into what remains, so a long right side costs the left its room. `path-grants` and `exposed-ports` are the two that shrink to fit, trading their last entries for a `+3`; every other segment is drawn whole or dropped. The centre is centred against the full width and dropped where it will not sit there without colliding, so it suits something short and steady.

`/info` draws every segment there is with its current value, naming those drawing nothing, which is how to judge one before putting it in the bar.

| Segment              | Draws                                                                 | Options                                     |
|----------------------|-----------------------------------------------------------------------|---------------------------------------------|
| `activity-spinner`   | `frames` in turn while a turn runs, and `idle` otherwise              | `idle`, `frames`, `rate`                    |
| `active-model`       | the model, its effort as a ladder of squares, and `⚡` when fast      | none                                        |
| `cache-usage`        | what share of the last request the provider read from cache           | none                                        |
| `context-usage`      | the context filled, as a percentage and used over total tokens        | none                                        |
| `exposed-ports`      | ports as links, prefixed by associated jobs; `⇠` marks routes to host | none                                        |
| `fast-mode`          | `⚡` for the fast model, `·` for the standard one                     | none                                        |
| `git-branch`         | the workspace's branch, or a short hash when detached                 | `rate`, default `5s`                        |
| `jobs`               | a mark and name per job, a finished one lingering 30 seconds          | none                                        |
| `local-time`         | the clock, its refresh following the format's finest field            | `format`, a Go layout, default `15:04`      |
| `mode-toggle`        | the capability letters, lit where granted and dim where not           | none                                        |
| `path-grants`        | each granted path with its access flags, linked to the path it names  | `type`: `base`, `short`, or `full`          |
| `scroll-overflow`    | how many input lines are hidden that way, and nothing where none are  | `direction`: `up` or `down`, and no default |
| `session-emoji`      | the emoji drawn from the session name                                 | none                                        |
| `session-name`       | the session name linked to its directory                              | `emoji`, `true` to append it                |
| `session-spend`      | what the session has cost, in `ui.currency`                           | none                                        |
| `subscription-usage` | a gauge per subscription window, with its freshness and any limit     | `rate`, default `5m`                        |
| `turn-count`         | `#n`, and nothing before the first turn                               | none                                        |
| `turn-timer`         | minutes waited then minutes worked, the running one of the two lit    | none                                        |
| `workspace-dir`      | the workspace directory, linked to it                                 | `type`: `base`, `short`, or `full`          |

A `rate` or a duration takes Go's form, as `125ms`, `10s`, or `5m`. A segment refusing an option says which position it sits in and what the option wanted instead, and startup stops there.

## Capability Flags

`caps.default` is a string of flags, defaulting to `rx`, and applies when the command line names none. Read is implied whatever the string says.

| Flag | Grants                                               |
|------|------------------------------------------------------|
| `r`  | Read files within the granted paths                  |
| `w`  | Write the workspace, excluding anything under `.git` |
| `x`  | Run shell commands                                   |
| `n`  | Network beyond the sandbox's private loopback        |
| `g`  | Write under `.git`, which `w` alone refuses          |
| `l`  | The lookup tool                                      |

The user toggles capabilities at runtime, so `caps.default` is a starting posture, not a ceiling.

Grant the narrowest set that does the job. Leave `caps.default` to the user: propose a string and name the capabilities it adds.

## Custom Tools

`[tools.<name>]` adds a custom tool. The model calls it like a built-in tool. The tool runs the command in `command`.

The table name is the tool name. Use lowercase letters, digits, and underscores. Start with a letter. A name that a built-in tool already uses stops startup.

The command runs on the host, in the workspace directory. The sandbox does not confine it. The capability flags do not gate it. Only `permission` holds it back.

Put custom tools in the global config. A workspace `oh.toml` that sets `[tools]` stops startup.

| Key           | Holds                                                     |
|---------------|-----------------------------------------------------------|
| `description` | what the tool does; required                              |
| `command`     | the executable and its fixed arguments; required          |
| `parameters`  | the arguments the model supplies, each one a table        |
| `subject`     | the parameter shown in the call row; the first by default |
| `timeout`     | the limit for one call; `30s` by default                  |
| `permission`  | `ask` by default, or `allow` to run without a question    |

The model reads the description alone to choose a tool. Write it for the model.

With `ask`, oh shows the command line and waits for a yes or a no. A no tells the model to try something else. Print mode has nobody to ask, so the call fails.

Give each parameter a `name`, a `kind`, and a `description`. Add `optional = true` where the model can leave it out. Add `values` for an enum.

| Kind      | Passes                                           |
|-----------|--------------------------------------------------|
| `string`  | `--name value`                                   |
| `integer` | `--name 7`                                       |
| `boolean` | `--name` when true, and nothing when false       |
| `strings` | `--name` once for each item                      |
| `enum`    | `--name value`, refused unless `values` holds it |

An underscore in a parameter name becomes a dash in the option, so `max_lines` arrives as `--max-lines`. An optional parameter the model leaves out passes nothing. The command reads the tool name from `OH_TOOL`.

```toml
[tools.weather]
description = "report the weather for a city"
command = ["./tools/forecast"]
subject = "city"
timeout = "10s"
permission = "ask"
parameters = [
    { name = "city", kind = "string", description = "the city to report on" },
    { name = "days", kind = "integer", description = "how many days ahead to look", optional = true },
    { name = "units", kind = "enum", values = ["metric", "imperial"], description = "which units to report in", optional = true },
]
```

oh resolves the first word of `command` against the config file when the word holds a `/`. It resolves a later word the same way when the word starts with `./` or `../`. It leaves a bare word to `PATH`. A command oh cannot find stops startup. The message names the config file and the tool.

The result holds standard output and standard error. A non-zero exit reports a failure with that output. A timeout reports a failure the same way.

`-t` selects the tools for a session, custom tools included. `-t weather` offers that tool alone. The session freezes its tool set, so a change here reaches the next session.

## Sandbox Paths

`[sandbox]` decides which user paths exist; `[caps]` decides what may be done with them. The six structured path tools (`read`, `ls`, `find`, `grep`, `write`, and `edit`) share one mounted filesystem root. Sandboxed `bash` and `job` policies are built from the same prepared grants.

- `deny` — file or directory name globs that path tools and confined shell commands cannot access anywhere; patterns contain no path separator, and a matched directory denies its whole tree
- `read` — read-only to path tools and shell commands
- `write` — readable and writable to path tools and shell commands, apart from any `.git` within it, which only `g` makes writable
- `exec` — read-only to path tools, and readable plus executable to shell commands
- `path` — the same as `exec`, and appended to the shell's `PATH`
- `home` — expose a real-home file at the same relative location in private `HOME`; it is read-only to path tools and shell commands

Access combines when the same path is named more than once: `write` adds writing and `exec` or `path` adds execution.

Two implicit sets use the same source functions for both enforcement paths: the system paths needed to run commands (such as `/usr` and selected runtime configuration under `/etc`), and existing directories inherited through `PATH`. Path tools can read both sets; shell commands can also execute the executable directories. Beyond those sets and the exceptions below, a path absent from `[sandbox]` is unreachable however generous the caps.

A confined shell receives a few runtime grants that do not represent ordinary user paths: its namespaced `/proc`, pseudoterminals, synthetic resolver files, host language-package proxy cache, and writable runtime devices. Private `.cache` and `/tmp` are writable to both path tools and shell commands. Clipboard drops and global skill roots are mounted for path tools specifically and are identified that way in the prompt; they are not general shell grants. Under `--yolo`, only shell commands become unconfined—structured path tools retain their mounted-root restrictions. A confined `job` uses the same filesystem policy as `bash`, but always stays on private loopback networking; only a foreground `bash` call can request `network=host`.

```toml
[sandbox]
deny = [".env", "*.pem", ".ssh"]
read = ["~/src/reference"]
write = ["~/.config/org.crdx/oh/skills"]
exec = ["/opt", "~/.local/share/toolchains"]
path = ["~/.local/state/org.crdx/toolbox/bin"]
home = ["~/.config/git/ignore"]
```

An `exec` grant makes a directory runnable, not findable: the shell inherits the `PATH` oh was launched with, so a tool granted that way is reachable only by its full path. `path` grants the same and puts the directory on `PATH` as well, which is what a directory of binaries wants. It is appended rather than prepended, so nothing there can shadow a host tool of the same name.

Deny patterns are checked directly by path tools. Before the first confined command uses a set of grants, oh finds matching paths across its reachable trees and reuses that result for later commands with the same grants. A symlink cannot provide another route to a denied name. Under `--yolo`, deny rules bind only the path tools: oh warns at startup that the unconfined `bash` and `job` tools can reach a denied path, and tells the agent never to use them for one.

Every setting here is read once, when oh starts. Editing `[sandbox]` changes nothing about the session running now, in either direction, so tell the user the grant waits for oh's next run and that resuming this conversation in that run is enough to collect it.

## Skills

A skill is a directory holding `SKILL.md`, and the directory name is the skill's name.

This one is built in, written to `<state>/skills/oh/SKILL.md` at startup and rewritten when the binary's content differs; `<state>` is `OH_STATE_DIR` or `~/.local/state/org.crdx/oh`. Change it at `internal/app/skill/skills/oh/SKILL.md` in the repository; the next startup overwrites any edit to the copy.

Project skills live in `<workspace>/.agents/skills/<name>/SKILL.md`, discovered automatically. A workflow confined to one repository belongs here.

Global skills live in `~/.config/org.crdx/oh/skills/`, always scanned, plus every directory in `skills.include`. Roots deduplicate by resolved symlink.

`exclude` takes an individual skill's directory rather than a root, and drops global skills, the built-in one included. A project skill cannot be excluded.

```toml
[skills]
include = ["~/src/dotfiles/oh/skills"]
exclude = ["~/.local/state/org.crdx/oh/skills/oh"]
```

Discovery warns and skips rather than failing. A skill with no description is dropped. A `name` disagreeing with its directory loses to the directory. Two roots may supply the same name, so remove the duplicate rather than relying on either winning.

## Source

oh is built from the root of the `crdx.org/oh` module. Its importable primitives live under `pkg/`, and its command-only packages live under `internal/app/`. Read them to settle a question this skill leaves open.

oh points `GOPROXY` at the module cache on disk, so an installed version extracts without network:

```bash
go mod download -json crdx.org/oh@latest
```

`Dir` is the extracted tree, read-only. `Version` is what `@latest` resolved to, which is the newest version the cache holds rather than the newest released.

Read a dependency's source the same way, naming that module.

`go env GOMODCACHE` names the session's own cache rather than the user's, so download the module rather than going looking by hand.

Confirm the version before trusting what you read:

```bash
go version -m "$(command -v oh)"
```

Where the two disagree the source is older than the running oh, and behaviour it lacks may be newer. Name the version read.

Where the cache holds no `crdx.org/oh`, ask whether a checkout is granted. One named `oh` is the front page and carries no code; the code is in one named `io`.

## Changing A Setting

### 1. Choose Layer

Global `config.toml` for a setting that follows the user everywhere; the project's `oh.toml` for one true of that project alone. Capability defaults belong at workspace level.

### 2. Verify Key

Confirm the table and field here first, then in `defaults.toml` and `config.go` where the source is granted.

### 3. Write File

Write only the keys that differ. Comment a capability grant with what it withholds.

### 4. Report Change

Name the capabilities the change adds, and that a workspace file persists into every future session in that project. Say which of the three the setting is — live now, at oh's next start, or in a new session — and never blur the last two together.

## Session Layout

Each session is a directory under `<state>/sessions/`, holding some of:

| File            | Holds                                                       |
|-----------------|-------------------------------------------------------------|
| `chat.md`       | the conversation, human-readable — read this first          |
| `session.jsonl` | the journal, one record per line, and the source of truth   |
| `meta.json`     | the listing metadata, including when it was last written to |
| `wire.http`     | recorded provider traffic, large and unsummarisable         |
| `drops/`        | pasted images and saved tool output                         |

Each session has a scratch directory under `<state>/farm/<name>/`, holding the tree copy and patches of an agent working in a read-only workspace.

## Session Liveness

A session is either running or ended, and nothing inside its directory says which. `oh --ctl sessions` says:

```bash
oh --ctl sessions              # every stored session
oh --ctl sessions --running    # only the ones still going
oh --ctl sessions -w .         # only the ones in this workspace
oh --ctl sessions --json       # the same listing, with the session and scratch directories in it
```

The status comes from the lock the running process holds on the journal. Everything else is ambiguous: a last message an hour old reads the same whether the session ended then or has worked ever since, modification times inside a sandbox may all report when the tree was mapped, and a running session that is thinking has not written recently. Read `touched` in `meta.json` only where `oh --ctl` cannot run.

`oh --ctl` resolves the state directory through `OH_STATE_DIR`, which oh sets inside its sandbox. It needs the sessions path granted; ask the user where it is not.

Describe a running session as running, and its scratch directory as work in hand. Treat the end of a transcript as where the record stops; nothing marks a clean close. Check liveness before drawing any conclusion from a scratch directory, including whether its work has landed.

## Reading Sessions

### 1. Determine Scope

Take both from the invocation rather than asking:

- `N`, how many sessions: 5, or the number the user named.
- The query, where the user gave one, which is the focus of every summary. With none, summarise generally.

### 2. Find Sessions

`oh --ctl sessions --json` gives the newest first, with the status, title, workspace, and directories of each. Take the top `N`, dropping your own session, which is in the listing and running.

Where a session has no `chat.md`, read `session.jsonl` for that one.

### 3. Size Sessions

`wc -l` each. Read a transcript under a few hundred lines whole, and anything larger by range.

### 4. Read Sessions

Work newest first, reading only what answers the question:

- `tail -n 200` for what the session is doing now.
- `head -n 60` where the tail leaves the subject unclear, for what it set out to do and in which project.
- With a query, `grep -n -i` for it and read around each hit with `sed -n '<start>,<end>p'`.

Read the next session before deepening the last: breadth across `N` sessions beats depth in one. Note a session unrelated to the query and move on.

### 5. Report Findings

Digest newest first: a heading per session naming it, its status, and its subject, then what was done or decided and any loose ends. With a query, lead with the cross-session answer, then the per-session detail.

Where catchup precedes another task, carry the context straight into it.
