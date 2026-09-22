# Changelog

## [N.N.N] - XXXX-XX-XX

### Changes

- Improve rendering performance of reasoning
- Put panels in their own group
- Make the job end message read more like a sentence
- Shorten and simplify the restart job hint message
- Measure grapheme clusters accurately
- Keep a queued message on screen while it sends
- Drop the pace figure from usage
- Mark a limited window with ⊘
- Add custom tools
- Isolate IPC and UTS namespaces
- Fix turn resumption after a night's sleep
- Document the denied path weirdness for now

## [0.8.0] - 2026-09-20

### Changes

- Rename the Go module to `crdx.org/oh`

## [0.7.2] - 2026-09-20

### Changes

- Suppress desktop notifications while the terminal is focused

## [0.7.1] - 2026-09-19

### Changes

- Tweak the simulator

## [0.7.0] - 2026-09-19

### Interface

- Format command-line errors
- Shorten and standardise runtime messages
- Highlight `Targets` header in the same colour as `Commands`
- Render required width for wide Mermaid diagrams
- Truncate a tall footer so it doesn't flicker
- Mark user messages with OSC 133 magic markers

### Input

- Keep queued messages visible at all times
- Hint that double enter sends queued messages now
- Dismiss feedback with backspace, escape, or ^D
- Draw feedback in an input frame box
- Fence piped input when there is also a prompt

### Approvals

- Highlight URLs in red in host networking prompt
- Reword the host networking prompt
- Drop warning chevron from approval prompt header
- Send a desktop notification on approval prompt
- Don't include prompt waiting time in tool call time

### Hyperlinks

- Linkify the workspace-dir bar segment
- Linkify the path-grants bar segment
- Linkify the session-name bar segment
- Shorten model scratch paths with `<s>` prefix
- Include line range in read tool hyperlinks

### Jobs

- Limit background job name length and format
- Clarify job wait stopped reason to all parties
- Render `/jobs` commands like bash tool calls

### Sessions

- Regenerate transcripts inside archived sessions
- Tell forks to follow the chain of forks
- Remove trailing new line at the end of chat transcript

### Maintenance

- Move session maintenance to `oh --ctl`
- Remove the `ohctl` command

### Garbage Collection

- Report real space on disk
- Find caches by their contents
- List nuked caches, largest first
- Delete dangling farm dirs
- Delete rebuildable Go binaries when aggressive
- Leave the shared home alone while a session is running

### Models

- List changed models on update
- List changed models after a startup refresh

### Configuration

- Refuse invalid config files
- Ensure post-onboarding config is valid

### Tools

- Tell the model how to use title and notify
- Preserve raw fetch result in a session drop
- Remove the built-in mise support

### Sandbox

- Run the demo unsandboxed since it's safe

## [0.6.0] - 2026-09-18

### Theme

- Customise the look and feel with theme support
- Themes apply instantly, so just ask the model to theme oh right in front of your eyes
- Use project-level `oh.toml` to define a theme per project
- Decorate any theme key with bold, faint, italic, underline, blink, reverse, hidden, strikethrough, and overline
- Give an underline its own shape and colour with `underline:curly` and `underline:#rrggbb`
- Reference theme change as `ui.theme` rather than every single colour in the universe

### Permissions

- Add `[permissions]` for granular control
- Ask before running risky tools
- Ask for approval inside the input frame
- Allow bash within host network, toggled with `n` cap
- Remove the deprecated `s` cap
- Use the workspace read and write state to colour the `x` cap

### Security

- Add cached glob-based `sandbox.deny` names that remain inaccessible inside ordinary grants
- Refuse workspace config flags: editor, experimental, provider, sandbox, skills
- Refuse a bar segment option holding text the terminal would obey
- Refuse a `ports.hostname` that is not a hostname
- Draw a provider's model list without the control characters it may carry
- Paste without the control characters the clipboard may carry
- Resolve every configured path into its real path
- Preserve nested write grants inside read-only paths
- Keep unreadable nested directories read-only
- Omit redundant PATH execution grants for covered targets
- Keep deleted temporary path grants from disabling the shell
- Report redundant and conflicting paths
- Let a file tool follow a symlinked path
- Ensure maximum shell timeout is less than the typical prompt cache TTL
- Notify which config keys changed on auto-reload
- Remove `sandbox.host_loopback`

### Providers

- Show the endpoint's message when it quotes the error code as a number
- Handle a full context window with a hint to `/fork <model>`

### Tools & Skills

- Rename `web_search` to `lookup`, toggled with `l` cap
- Rename `web_fetch` to `fetch`, toggled with `n` cap
- Search the web at high reasoning effort
- Bound a lookup by silence rather than by total length
- Add max job wait time
- Limit background job cleanup to one second on close
- Save truncated remains in the session drops directory
- Shorten file refusal messages
- Serve a built-in `oh` skill
- Detail the read-only workflow better
- Tell the model it can ask for hugh `/grant`
- Update `notify` to state that its inputs are plain text

### Usage

- Use fresh usage reset times rather than cached ones
- Show when a limited usage window resets
- Refresh frozen limits in case of an external reset (hello OpenAI)
- Fix detection of usage window timespans
- Fix detection of Codex context windows
- Pick at least high effort level when not specified

### Interface

- Add `/!` to run a command on the host
- Wrap long pastes in a fenced code block
- Try to detect the language of the paste
- Store request failures in a structured format
- Show an input frame beneath session previews
- Support theme colour customisation
- Theme harness-submitted messages
- Gather consecutive mid-round notices into one panel
- Handle pending notices correctly
- Tell the model exactly what the harness drew
- Deliver harness notices before turn end
- Submit pending notices on a double enter
- Hint above standing notices that a double enter sends them
- Gather consecutive harness notices into one block
- Display each capability change as its own notice
- Display each machine condition change as its own notice
- Shorten harness notices in general
- Repair the region if the terminal is too short
- Show whole seconds for durations >= 1s
- Remove some character-building options
- Detect environment changes on resume
- Linkify paths containing spaces

### Analysis

- Analyse every token and provider
- Only look at valid journals
- Draw a markdown heading above each table

## [0.5.0] - 2026-09-10

### Changes

- Detect valid Codex models the right way
- Default to high effort level, and make it configurable
- Make fast mode default configurable
- Remove stray blank line from incoming mid-round notices
- Spell grant flags in capability order (`rxw`)
- Warn when [sandbox.read] is used redundantly
- Linkify paths in tool calls, reasoning, notices, everything!
- Prevent sub usage flashing on refresh

## [0.4.0] - 2026-09-10

### Changes

- Preview sessions in session picker
- Name sandbox helper processes
- Restore the cache properly on resume
- Fix linkification of paths ending in dots

## [0.3.0] - 2026-09-10

### Changes

- Add LLM simulator backend with `--demo`, or use the _Simulation_ option during onboarding
- Track input and output token API prices, stored when model list is updated
- Add session spend segment which shows the API cost of the current session
- Configure currency with `ui.currency` if you don't like US dollars
- Now `ohctl analyse` also reports models, spend, conversation, faults, and tool use
- Render markdown images inline
- Map /tmp paths within sandbox to host paths transparently
- Add timestamps and response decompression state to `wire.http` logs
- Make an unknown `config.toml` key be just a warning
- Read OpenAI subscription usage from account if stale on startup
- Fix some minor text wrapping, truncation, spacing, and hyperlink issues
- Stop tool call rows getting truncated an extra two chars
- Fix image rendering when several parallel reads are triggered
- Remove the extra blank line that was sometimes left behind on exit
- Shorten notices and format token counts consistently
- Remove the superfluous space before the image address

## [0.2.0] - 2026-09-09

### Wire Protocols

- Anthropic Messages
- OpenAI Chat Completions
- OpenAI Responses

### Providers

- Anthropic (Claude subscription)
- Codex (ChatGPT subscription)
- OpenCode Go
- Ollama (local, network)

### Models

- Listing with `-l`
- Caching, refreshed when stale
- Legacy models filtered out
- Incompatible models filtered out
- Round-robin rotation
- Effort levels
- Fast mode

### Tools

- `job`: run background commands
- `notify`: send desktop notifications
- `title`: name the session
- `web`: search and fetch pages
- `expose`: forward a port inwards
- Restrict with `-t`
- Configurable output cap
- Malformed call correction
- Out-of-band result viewing
- Batched concurrent calls
- Stale-file edits refused

### Sandbox

- Namespaces, Landlock, Seccomp
- Virtual `/proc` and private `/tmp`
- Grants resolved per component
- Symlinks never followed
- File-level read, write, exec grants
- Process count limits
- File size limits
- Output size limits
- Processor time limits
- Port forwarding in both directions
- Session-scoped grants
- Grant management and revocation
- Offline Go module proxy
- Obviously, `--yolo` for the daring

### Capabilities

- Read always granted
- Shell execution opt-in
- Workspace writes opt-in
- Repository history opt-in
- Web access opt-in
- Mid-session toggling with ctrl+x
- Recorded in the session, restored on resume

### Interface

- Configurable input block segments:
    - Model
    - Fast Mode
    - Context
    - Usage
    - Cache Share
    - Turn Timer
    - Turn Count
    - Time
    - Git Branch
    - Session Name
    - Session Emoji
    - Workspace Directory
    - Path Grants
    - Exposed Ports
    - Jobs
    - Mode Toggle
    - Activity Spinner
    - Scroll Overflow
- Streaming modes:
    - ASAP
    - Line
    - Paced
- Reasoning display:
    - Plain
    - Markdown
- Configurable output grouping
- Incremental Markdown rendering
- Mermaid diagrams from fenced blocks
- Syntax highlighting
- OSC 8 links
- Message copying over OSC 52
- Pictures drawn with kitty graphics
- Terminal title management

### TUIs

- Model picker
- Session picker
- Onboarding wizard

### Input

- Multi-line editing
- Scrollable input
- History recall ("reverse-i-search")
- Pasted text handled sanely
- Pasted images handled

### Commands

- `/conf`: edit the configuration
- `/copy`: copy a target
- `/edit`: edit a target
- `/info`: show the session
- `/open`: open a target
- `/new`: start a session
- `/expose`: expose a host port
- `/grant`: grant path access
- `/grants`: list grants and routes
- `/revoke`: revoke a grant or route
- `/jobs`: list background jobs
- `/job`: manage a background job
- `/help`: list the commands
- `/fork`: fork the session
- `//double-slash` snippets
- Unknown commands rejected
- Tab completion

### Agent Turns

- Message queueing
- Mid-turn interjection
- Pokes on an unanswered turn
- Idle-timeout monitoring
- Retries with backoff
- Retriable HTTP 507
- Usage-limit detection
- Automatic recovery probing
- Cache-loss reporting
- Cache-rebuild reporting
- Prompt-prefix violation reporting
- Notifications on turn finish
- Notifications on session death

### Sessions

- Versioned journal
- Migrations, locking
- Corrupted journals refused
- Frozen:
    - Tools
    - Model
    - Workspace
    - Confinement
    - Capabilities
- Archiving
- Restoring
- Deletion
- Forking
- Source chat drops
- Compact Markdown transcripts
- HTTP wire logs
- Transcript regeneration

### Configuration

- Fully XDG compliant
- Local `oh.toml` overrides (lists merged)
- Paths resolved per file
- Ordered settings replaced
- Local settings named at startup
- Defaults with `caps.default`
- Live reloading
- Skills, skill include and exclude patterns

### Command Line

- Session resumption with `-r`
- Model selection with `-m`
- Capability selection with `-c`
- Print mode with `-p`
- Initial files with `--add`
- Piped stdin joined with arguments
- OAuth login with `-L`
- Sub usage reporting

### Maintenance

- `ohctl sessions`
- `ohctl analyse`
- `ohctl regenerate`
- `ohctl migrate`
- `ohctl gc`

### Development

- Replay-based test suite
- Terminal goldens

### Linters

- `abbreviation`
- `adjective`
- `boolname`
- `receivername`
- `stdstream`

## [0.1.0] - 2026-08-17

Initial release.

Several primitive top-level packages:

- `agent`: conversation loop, with streaming, batching, and cancellation
- `tool`: tools, schemas, middleware, concurrency, and orchestration
- `toolbox`: implementation of read/ls/find/grep/write/edit/bash
- `session`: session saving and resumption, as an append-only journal
- `provider/codex`: the Responses API, the OpenAI way (for now)

Some tools:

- `cmd/login`: do the standard OAuth handshake and store the credentials
- `cmd/simulate`: serve a defined scenario as a simulation of the Responses API

A few examples:

- `cmd/weather`: define a tool, then ask a question that needs it
- `cmd/streaming`: print each event of a turn as it arrives
- `cmd/simple`: the same loop, with text fragments glued back into whole messages

A harness:

- `cmd/oh`: opinionated af
