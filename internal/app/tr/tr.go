// Package tr holds strings.
package tr

// The keyboard shortcuts reference that ctrl+/ opens. Each action follows its keys, as in
// "^D  Quit from an empty draft". The footer shows as "^/ or ^C closes · ↑/↓ scrolls".
const (
	ShortcutHelpTitle       = "Keyboard shortcuts"                          // heads the reference
	ShortcutHelpFooter      = "%s closes · %s scrolls"                      // the last line, with the keys that close and scroll
	ShortcutEditingSection  = "Editing"                                     // heads the editing keys
	ShortcutAccessSection   = "Access: press %s, then a letter"             // heads the access toggles, as in "Access: press ^X, then a letter"
	ShowOrCloseShortcuts    = "Show or close this reference"                // beside ^/ or ^?
	CancelQuestion          = "Cancel the question"                         // beside Escape / ^C while a question waits
	ChooseSelectedAnswer    = "Choose the selected answer"                  // beside Enter while a question waits
	SelectAnswer            = "Select an answer"                            // beside Left / Right / Tab while a question waits
	ScrollQuestionDetails   = "Scroll the question details"                 // beside Up / Down while a question waits
	CancelExternalEditing   = "Cancel external editing"                     // beside Escape / ^C / ^D while the editor is open
	LeaveSearch             = "Leave the search"                            // beside Escape during a history search
	FindPreviousMatch       = "Find the previous match"                     // beside ^R during a history search
	ClearSearchAndDraft     = "Clear the search and the draft"              // beside ^C twice during a history search
	ShortenSearch           = "Shorten the search"                          // beside Backspace during a history search
	StopCommandThenTurn     = "Stop the foreground command, then the turn"  // beside Escape / ^D while a turn or command runs
	QuitFromEmptyDraft      = "Quit from an empty draft"                    // beside ^D while nothing runs
	ClearDraft              = "Clear the draft without stopping work"       // beside ^C twice / ^U
	InsertNewline           = "Insert a newline"                            // beside Shift+Enter / ^J
	QueueDraft              = "Queue the draft"                             // beside Enter while a turn runs, or Alt+Enter with the enter-interrupts experiment
	StopTurnAndSendDraft    = "Stop the turn and send the draft"            // beside Alt+Enter while a turn runs, or Enter with the enter-interrupts experiment
	SendDraft               = "Send the draft"                              // beside Enter / Alt+Enter while nothing runs
	TakeBackQueuedMessage   = "Take back the newest queued message"         // beside ^Z / Escape / ^D while messages are queued
	SendQueuedMessages      = "Send the queued messages"                    // beside Enter twice while messages are queued
	TakeBackLastTurn        = "Take back the last turn"                     // beside ^Z when the last turn can be undone
	ContinueOrSendNotices   = "Continue, or send the pending notices"       // beside Enter twice while nothing runs
	CompleteWord            = "Complete a word or choose a suggestion"      // beside Tab
	MoveByLine              = "Move by line or recall the history"          // beside Up / Down
	SearchInputHistory      = "Search the input history"                    // beside ^R
	MoveToLineStart         = "Move to the start of the line"               // beside Home / ^A
	MoveToLineEnd           = "Move to the end of the line"                 // beside End / ^E
	MoveByWord              = "Move by word"                                // beside ^Left / ^Right
	DeletePreviousWord      = "Delete the previous word"                    // beside ^Backspace / ^W
	DeleteNextWord          = "Delete the next word"                        // beside ^Delete
	DeleteToLineEnd         = "Delete to the end of the line"               // beside ^K
	EditDraftExternally     = "Edit the draft externally"                   // beside ^G
	StashDraft              = "Stash the draft or restore the latest stash" // beside ^S
	SwapDraftWithStash      = "Swap the draft with the latest stash"        // beside ^Shift+S
	RedrawConversation      = "Redraw the conversation"                     // beside ^L
	ReplyInEditor           = "Reply to the last answer in the editor"      // beside ^. when the model has answered
	CancelModePrefix        = "Cancel the mode prefix"                      // beside Escape / ^C after ^X
	ToggleWorkspaceWrites   = "Toggle workspace writes"                     // beside w, in a sandboxed session
	ToggleShellExecution    = "Toggle shell execution"                      // beside x, in a sandboxed session
	ToggleHostNetwork       = "Toggle host network"                         // beside n, in a sandboxed session
	ToggleRepositoryHistory = "Toggle repository history"                   // beside g, in a sandboxed session
	ToggleSubagents         = "Toggle subagents"                            // beside s
	ToggleLookup            = "Toggle lookup"                               // beside l
	ToggleCustomToolGroup   = "Toggle this custom tool group"               // beside the letter of each custom tool group
)

// The descriptions of the slash commands, in the completion list and in /help.
const (
	HostCommandDescription = "run a command on the host and tell the model what it printed"                // for /!
	ConfDescription        = "edit the config and system prompt"                                           // for /conf
	CopyDescription        = "copy a target to the clipboard"                                              // for /copy
	ContextDescription     = "list the sources of the model context"                                       // for /ctx
	EditDescription        = "open a target in the editor"                                                 // for /edit
	InfoDescription        = "show every bar segment with its current value"                               // for /info
	OpenDescription        = "open a target with its default application"                                  // for /open
	NewDescription         = "start a new session"                                                         // for /new
	ForkDescription        = "fork this session"                                                           // for /fork
	HelpDescription        = "list the commands and targets"                                               // for /help
	ReplyDescription       = "reply inline to the last answer in the editor"                               // for /reply
	ForwardDescription     = "forward a sandbox port to the host"                                          // for /forward
	GrantDescription       = "grant temporary access to a path"                                            // for /grant
	GrantsDescription      = "list effective access, forwarded ports and approvals"                        // for /grants
	RevokeDescription      = "revoke a listed target"                                                      // for /revoke
	TasksDescription       = "list this session's active background tasks"                                 // for /tasks
	TaskDescription        = "inspect, stop, restart or discard a background task, or prune finished ones" // for /task
	SubagentsDescription   = "list this session's running and latest finished subagents"                   // for /subs
	SubagentDescription    = "read a subagent's conversation"                                              // for /sub
	SnippetsDescription    = "list the configured snippets"                                                // for //help
)

// The list of snippets that //help shows. The heading comes above one row for each snippet.
const (
	SnippetsHeading = "Snippets:"                   // heads the list of snippets
	NoSnippets      = "No snippets are configured." // instead of the list when no snippet is configured
)

// The help that /help shows, and the targets of /copy, /edit and /open. A label names a target in a
// failure, as in "Session log does not exist yet".
const (
	CommandsHeading      = "Commands:"                      // heads the list of commands in /help
	TargetsHeading       = "Targets:"                       // heads the list of targets in /help
	CopiedTarget         = "Copied %s to the clipboard."    // after /copy of a text, as in "Copied last message to the clipboard."
	CopiedTargetValues   = "Copied %s to the clipboard: %s" // after /copy of a value, as in "Copied session name to the clipboard: wiry-turtle"
	TargetMissing        = "%s does not exist yet"          // when a target has no file yet, as in "Session log does not exist yet"
	TargetInspectFailure = "could not inspect %s"           // before the reason that a target cannot be inspected
	TargetReadFailure    = "could not read %s"              // before the reason that a target cannot be read
	SessionChatLabel     = "Session chat"                   // for session-chat and session-chat-file
	SessionLogLabel      = "Session log"                    // for session-log-file
	SessionDirLabel      = "Session directory"              // for session-dir
	SkillsDirLabel       = "Skills directory"               // for skills-dir
	ProjectContextLabel  = "Project context"                // for agents-file
	SystemPromptLabel    = "System prompt"                  // for the system prompt that /conf opens
)

// The sources of model context that /ctx lists. A heading comes above the sources of each kind.
const (
	NoContextSources      = "No context sources." // instead of the list when no source exists
	SystemContextHeading  = "System:"             // heads the sources from the harness
	ProjectContextHeading = "Project:"            // heads the sources from the workspace
	SessionContextHeading = "Session:"            // heads the sources from this session
)

// The background tasks that /tasks lists and /task acts on. A task shows its state and its time, as
// in "docs: running for 12s, run 2, respawns on exit".
const (
	NoActiveTasks      = "No active background tasks." // instead of the list when no task is active
	TasksHeading       = "Background tasks:"           // heads the list of tasks
	TaskRestarted      = "Restarted %s as run %d."     // after /task restart, as in "Restarted build as run 2."
	TaskDiscarded      = "Discarded %s."               // after /task discard, as in "Discarded docs: stopped."
	NoFinishedTasks    = "No finished tasks to prune." // after /task prune when no task has finished
	TasksPruned        = "Pruned %s."                  // after /task prune, as in "Pruned build, docs."
	StateForDuration   = "%s for %s"                   // a live task or subagent, as in "running for 12s"
	StateAfterDuration = "%s after %s"                 // a finished task or subagent, as in "done after 3m"
	SilentTask         = "%s with no output"           // a task that wrote nothing, as in "running for 12s with no output"
	TaskRun            = "run %d"                      // the run of a restarted task, as in "run 2"
	PreviousTaskRun    = "run %d: %s"                  // an earlier run in /task cat, as in "run 1: failed"
	TaskRespawns       = "respawns on exit"            // a task that starts again when it exits
)

// The subagents that /subs lists and /sub reads. A row shows the activity, as in "4 tool calls,
// last: Reading the parser".
const (
	NoSubagents              = "No subagents."                      // instead of the list when no subagent exists
	NoSubagentNamed          = "no subagent named %s"               // refuses /sub cat with an unknown name
	RunningSubagentsHeading  = "Running subagents:"                 // heads the running subagents
	FinishedSubagentsHeading = "Finished subagents:"                // heads the latest finished subagents
	EarlierSubagents         = "and %d earlier: %s; %s lists them." // below the list, as in "and 3 earlier: 2 done, 1 failed; /subs all lists them."
	ToolCallNoun             = "tool call"                          // the count of tool calls, as in "1 tool call", with "s" added for more than one
	LastIntent               = "%s, last: %s"                       // the activity of a running subagent, as in "4 tool calls, last: Reading the parser"
)

// The access that /grants lists. A heading comes above each kind of access. An approval row ends
// with its lifetime, as in "tool:fetch — Allow (session) (until you quit)".
const (
	NoGrants          = "No grants, forwarded ports or approvals." // instead of the list when nothing is granted
	DeniedHeading     = "Denied:"                                  // heads the path patterns that stay denied
	ForwardedHeading  = "Forwarded:"                               // heads the forwarded ports
	PathsHeading      = "Paths:"                                   // heads the granted paths
	ApprovedHeading   = "Approved:"                                // heads the remembered approvals
	UntilQuitLifetime = "until you quit"                           // the lifetime of an approval for the session
	RemainingLifetime = "%s remaining"                             // the lifetime of a timed approval, as in "4m 12s remaining"
	ApprovalRevoked   = "Revoked the approval for %s."             // after /revoke of an approval, as in "Revoked the approval for tool:fetch."
	NoApproval        = "no approval for %s"                       // refuses /revoke of an unknown approval
)

// The kinds of target that /revoke completes, beside each target, as in "8080  forwarded port".
const (
	ApprovalTarget      = "approval"             // beside a remembered approval, as in "tool:fetch  approval"
	PathGrantTarget     = "temporary path grant" // beside a path that /grant opened
	ForwardedPortTarget = "forwarded port"       // beside a port that /forward opened
)

// The session picker that oh --resume opens. Each action follows its key in the key line, as in
// "tab scope". A prompt asks for the same key again, as in "Press a again to archive wiry-turtle".
const (
	PickerScopeAction        = "scope"                                               // after tab, which switches between this workspace and every workspace
	PickerViewAction         = "view"                                                // after ← →, which switches between stored and archived sessions
	PickerPreviewAction      = "preview"                                             // after enter, which opens the preview
	PickerArchiveAction      = "archive/restore"                                     // after a, which archives a stored session or restores an archived one
	PickerDeleteAction       = "delete"                                              // after del
	PickerQuitAction         = "quit"                                                // after q
	AgentColumn              = "Agent"                                               // heads the column of session names
	AgentColumnQualified     = "Agent (%s)"                                          // heads the column of session names in a wider view, as in "Agent (all archived)"
	AllWorkspacesQualifier   = "all"                                                 // in AgentColumnQualified while every workspace shows
	ArchivedQualifier        = "archived"                                            // in AgentColumnQualified while archived sessions show
	WorkspaceColumn          = "Workspace"                                           // heads the workspace column in the view of every workspace
	TitleColumn              = "Title"                                               // heads the column of session titles
	ModelColumn              = "Model"                                               // heads the column of models
	EffortColumn             = "Effort"                                              // heads the column of effort levels
	MessagesColumn           = "Messages"                                            // heads the column of message counts
	SizeColumn               = "Size"                                                // heads the column of journal sizes
	LengthColumn             = "Length"                                              // heads the column of session durations
	LastMessageColumn        = "Last message"                                        // heads the column of times since the last message
	UntitledSession          = "(untitled)"                                          // in the Title column of a session with no title
	RestorePrompt            = "Press %s again to restore %s"                        // asks for the key again before a restore, as in "Press a again to restore wiry-turtle"
	ArchivePrompt            = "Press %s again to archive %s"                        // asks for the key again before an archive, as in "Press a again to archive wiry-turtle"
	DeletePrompt             = "Press %s again to delete %s"                         // asks for the key again before a delete, as in "Press delete again to delete wiry-turtle"
	RestoreProgress          = "Restoring…"                                          // while a session is restored
	ArchiveProgress          = "Archiving…"                                          // while a session is archived
	DeleteProgress           = "Deleting…"                                           // while a session is deleted
	OtherWorkspaceLabel      = "other workspace"                                     // starts the key line of a preview from another workspace
	OtherWorkspaceMessage    = "This session belongs to %s"                          // in the input row of a preview from another workspace, as in "This session belongs to ~/proj/far"
	OtherWorkspaceError      = "session %s belongs to another workspace"             // refuses to open a session from another workspace
	NoStoredConversations    = "there are no stored sessions"                        // refuses to open the picker when no session exists
	RebuildingSessionListing = "writing the session listing again from the journals" // while the listing of sessions is written again from the journals
)

// A menu, such as the model picker that oh -m opens. Each action follows its keys in the key line,
// as in "↑↓ move". The filter row shows the typed filter after its prompt, as in "Filter: opus".
const (
	MenuFilterPrompt = "Filter: " // starts the filter row
	MenuFilterAction = "filter"   // after /, at the left of the key line
	MenuMoveAction   = "move"     // after ↑↓
	MenuChooseAction = "choose"   // after enter
	MenuQuitAction   = "quit"     // after q
)

// The model picker that oh -m opens. The key line shows the sort order after s. The Model and
// Effort columns take ModelColumn and EffortColumn.
const (
	ModelEffortAction  = "effort"     // after ← →, which changes the effort level of the chosen model
	ModelSortAction    = "sort: %s"   // after s, with the current sort order, as in "sort: cost ↑"
	CheapestFirstOrder = "cost ↑"     // the sort order with the cheapest model first
	DearestFirstOrder  = "cost ↓"     // the sort order with the most expensive model first
	ProviderOrder      = "provider"   // the sort order that groups the models by provider
	ProviderColumn     = "Provider"   // heads the column of providers
	ContextColumn      = "Context"    // heads the column of context window sizes
	CostColumn         = "Cost"       // heads the column of estimated costs, with an arrow when sorted by cost, as in "Cost ↑"
	InputColumn        = "Input"      // heads the column of input prices
	OutputColumn       = "Output"     // heads the column of output prices
	IdentifierColumn   = "Identifier" // heads the column of model identifiers
)

// The report that oh -u prints when it updates the model list. The tables take ProviderColumn and
// ModelColumn. The hint comes after the list, as in "Run oh -u -I to see the 3 models that were
// ignored.", and the command between its lead and its end takes its own style.
const (
	ModelsColumn        = "Models"                                   // heads the count of models that a provider lists
	SelectableColumn    = "Selectable"                               // heads the count of models that the picker offers
	IgnoredColumn       = "Ignored"                                  // heads the count of ignored models
	SourceColumn        = "Source"                                   // heads the column that names where the list came from
	ReasonColumn        = "Reason"                                   // heads the column of reasons that a model was ignored, with -I
	ChangeColumn        = "Change"                                   // heads the column of models added or removed since the last update
	IgnoredChange       = "ignored"                                  // after a model added but not offered, as in "added (ignored: unknown id)"
	IgnoredHintLead     = "Run "                                     // before the command in the hint
	IgnoredHintEnd      = " to see the model that was ignored."      // after the command when one model was ignored
	IgnoredHintEndCount = " to see the %d models that were ignored." // after the command when more than one model was ignored
)

// The list of sessions that oh --ctl sessions prints. The table takes the column titles of the
// session picker, after a Status column. A note goes to standard error when nothing is listed.
const (
	StatusColumn             = "Status"                                                                                        // heads the column of session states, as in "running"
	NoStoredSessions         = "there are no stored sessions to list"                                                          // when no stored session exists
	NoArchivedSessions       = "there are no archived sessions to list"                                                        // when no archived session exists, with --archived
	NoStoredSessionMatches   = "no stored session matches %q"                                                                  // when the filter matches no stored session, as in `no stored session matches "kimi"`
	NoArchivedSessionMatches = "no archived session matches %q"                                                                // when the filter matches no archived session, with --archived
	ListingLimit             = "listing the newest %d of %d sessions, which a filter of more than %d characters lists in full" // when the list stops at its limit, as in "listing the newest 50 of 57 sessions, …"
	SessionDeleted           = "deleted session %q"                                                                            // after --delete, as in `deleted session "wiry-turtle"`
	NoSessionToDelete        = "no stored or archived session named %q"                                                        // refuses --delete of an unknown session
	SessionDeleteFailure     = "could not delete session %q"                                                                   // before the reason that --delete failed, as in `could not delete session "wiry-turtle": permission denied`
	WorkspacePathFailure     = "could not resolve the workspace path"                                                          // before the reason that --workspace failed
)

// The refusals when oh starts with a session to resume or fork. The code passes in the flags and
// commands that a refusal names, as in "pass -f to restore and resume".
const (
	SessionRunning        = "session %s is running"                                                                              // when another oh holds the session
	SessionArchived       = "session %s is archived; pass %s to restore and resume"                                              // when -r names an archived session without -f
	SessionElsewhere      = "session %s belongs to %s and can only be opened there"                                              // as in "session wiry-turtle belongs to /proj/far and can only be opened there"
	ResumedModeMismatch   = "a resumed session opens in the mode it was left in, which was %s rather than %s"                    // when -c asks for other access, as in "which was rx rather than rwx"
	ResumedGroupsMismatch = "a resumed session opens with the custom tool groups it was left with, which were %q rather than %q" // when -c asks for other custom tool groups
	ResumedModelKept      = "a resumed session keeps its model; start a new session to choose another"                           // when -r comes with -m
	ResumedToolboxKept    = "a resumed session keeps its toolbox; start a new session to change it"                              // when -r comes with -t
	ResumedSandboxed      = "this session is sandboxed; resume it without %s"                                                    // when --yolo resumes a sandboxed session
	NewerFormatSession    = "%s is written in a newer journal format than this oh reads (format %d): upgrade oh"                 // as in "wiry-turtle is written in a newer journal format than this oh reads (format 24): upgrade oh"
	NewerFormatSessions   = "%d sessions are written in a newer journal format than this oh reads (format %d): upgrade oh"       // when more than one session has a newer format
	OlderFormatSession    = "%s is written in an older journal format: run `%s` to bring it up to format %d"                     // as in "wiry-turtle is written in an older journal format: run `oh --ctl migrate` to bring it up to format 24"
	OlderFormatSessions   = "%d sessions are written in an older journal format: run `%s` to bring them up to format %d"         // when more than one session has an older format
)

// The feedback after oh reloads a changed config file. The confirmation comes above one row for
// each file, as in "oh.toml: editor.command". A reach row names the settings that wait, as in
// "applies when oh next starts: grants.read".
const (
	ReloadConfirmation    = "Reloaded the config:"                                                  // heads the rows of changed files
	RemovedSource         = "%s: gone"                                                              // a removed file that set nothing, as in "oh.toml: gone"
	RemovedSourceDropping = "%s: gone, dropping %s"                                                 // a removed file and its settings, as in "oh.toml: gone, dropping ui.streaming"
	UnchangedSource       = "%s: no setting changed"                                                // a changed file that changed no setting
	NextRunReach          = "applies when oh next starts"                                           // before the settings that wait for a restart
	NextSessionReach      = "applies to new sessions"                                               // before the settings that wait for a new session
	UnknownSettings       = "Ignored unknown settings:"                                             // above one row for each unknown setting
	ReloadFailure         = "Could not reload the config: %s"                                       // when a changed config file is not valid, with the reason
	SnippetReloadFailure  = "Could not reload the snippets: %s"                                     // when the new snippets cannot replace the old ones, with the reason
	SubagentReloadFailure = "Could not reload the subagent settings, so the earlier ones stand: %s" // when the subagent settings cannot change, with the reason
)

// The hints and notes in the frame around the conversation. Each hint names its key. A retry row
// starts with its attempt number, as in "[#2] Request failed; retrying in 0.5s".
const (
	StopHint         = "%s to stop"                                                       // at the right of a running /! command, as in "esc to stop"
	SendHint         = "%s to send"                                                       // below the queued messages or pending notices, as in "double-enter to send"
	SendingHint      = "sending…"                                                         // below the queued messages while the turn stops to send them
	DismissCountdown = "(dismissing in %ds)"                                              // after a confirmation that goes away by itself, as in "(dismissing in 4s)"
	Retrying         = "[#%d] Request failed; retrying"                                   // when a request fails and oh retries at once
	RetryingIn       = "[#%d] Request failed; retrying in %s"                             // when a request fails and oh retries after a wait
	ContextFull      = "The context window is full."                                      // when the conversation no longer fits the model
	ContextFullHint  = "The context window is full. Run %s to continue in a new session." // as in "The context window is full. Run /fork -m opus to continue in a new session."
)

// The undo that ^Z does. The hint row shows a refusal with its reason, as in "cannot undo:
// background work is active". A failure shows in the feedback row with its reason.
const (
	UndoUnavailable           = "nothing to undo"                              // when no turn can be taken back
	UndoRefusal               = "cannot undo: %s"                              // when the last turn cannot be taken back, with the reason
	UndoProgress              = "undoing…"                                     // while the turn stops before it is taken back
	UndoFailure               = "Could not undo: %s"                           // when the undo fails, with the reason
	UndoFailureStopped        = "Could not undo; the turn remains stopped: %s" // when the undo fails after the turn stopped, with the reason
	NoMessageBlocker          = "this turn has no message"                     // a turn that the person did not start
	NoticesBlocker            = "this turn submitted pending notices"          // a turn that sent the pending notices
	BackgroundBlocker         = "background work is active"                    // while a subagent, a task or a /! command runs
	PendingBlocker            = "messages or notices are pending"              // while messages or notices wait to be sent
	NextMessageBlocker        = "another message was submitted"                // after a later message
	InterruptionBlocker       = "the turn stopped for another reason"          // after a stop that ^Z, escape or ^D did not cause
	ToolCallBlocker           = "the turn reached a tool call"                 // after the model called a tool
	StateChangeBlocker        = "the session state changed"                    // after another change to the session
	ConversationRestoreReason = "the conversation could not be restored: %s"   // in UndoFailure when the conversation cannot go back, with the reason
)

// The segments of the bar and the errors in their settings, as in "direction must be up or down,
// got sideways".
const (
	GitStatusFailure     = "status failed"                                                         // in the git status segment when git fails
	UsageUnavailable     = "usage unavailable"                                                     // in the subscription usage segment when the provider says nothing
	UnknownSegment       = "%s: there is no segment called %q, only: %s"                           // as in `top.left: there is no segment called "clock", only: gitBranch, gitStatus`
	RateNotPositive      = "rate must be positive, got %s"                                         // when a segment rate is zero or less
	RateNegative         = "rate must not be negative, got %s"                                     // when a segment rate is less than zero
	SpinnerFrameMismatch = "%q is %d cells wide and %q is %d, so the rule beside them would shift" // when the spinner frames differ in width from the idle frame
	SpinnerFramesEmpty   = "frames must not be empty; leave them out for the default spinner"      // when the spinner frames are an empty list
	SegmentTypeChoice    = "type must be omitted, %q, %q or %q, got %q"                            // when a path segment has an unknown type
	DirectionChoice      = "direction must be %s or %s, got %q"                                    // when the scroll segment has an unknown direction
)

// The errors in a --set setting on the command line. The flag and the setting come first, as in
// "--set ui.currency+=GBP: ui.currency does not hold a list of text, …".
const (
	SettingShape       = "write %s as setting=value or setting+=value"                       // as in "write --set as setting=value or setting+=value"
	SettingNameInvalid = "%q does not name one setting"                                      // as in `"grants read" does not name one setting`
	AppendedNotText    = "%s takes text or a list of text"                                   // after +=, as in "grants.read takes text or a list of text"
	AppendedNotList    = "%s does not hold a list of text, so nothing can be appended to it" // after +=, as in "ui.currency does not hold a list of text, …"
)

// The errors in a config file. The file and the setting come first, as in "oh.toml: input.nudge is
// empty". The code passes in each setting name.
const (
	SettingEmpty         = "%s is empty"                                                                   // as in "input.nudge is empty"
	SettingMissing       = "%s is missing"                                                                 // as in "rule is missing"
	SettingNotText       = "%s is not text"                                                                // as in "rule is not text"
	SettingNotDuration   = "%s is not a duration"                                                          // as in "timeout is not a duration"
	SettingDurationShape = "%s is not a duration such as %q"                                               // as in `timeout is not a duration such as "30s"`
	SettingNotPositive   = "%s must be positive, got %q"                                                   // as in `timeout must be positive, got "0s"`
	SettingNegative      = "%s must not be negative"                                                       // as in "timeout must not be negative"
	SettingMinimum       = "%s must be at least %d, got %d"                                                // as in "subagent.concurrency must be at least 1, got 0"
	SettingsInConflict   = "%s and %s cannot both be set"                                                  // as in "agent.model and agent.round_robin cannot both be set"
	UnknownSettingNames  = "unknown: %s"                                                                   // lists the unknown keys of a table, as in "unknown: rules, timer"
	UnknownToolSetting   = "unknown tool setting %s"                                                       // as in "unknown tool setting tool.bash.colour"
	WorkspaceOverride    = "%s cannot be overridden in %s"                                                 // as in "grants.read cannot be overridden in oh.toml"
	ControlCharacter     = "%s holds %q, which the terminal would read as an instruction rather than text" // as in `ui.prompt holds '\x1b', which the terminal …`
	BrokenDefaults       = "the built-in defaults are broken"                                              // before the reason that the defaults do not parse
	ConfigNeedsMigration = "config format %d needs migrating: run %s"                                      // as in "config format 3 needs migrating: run oh --ctl migrate"
	ConfigTooNew         = "config %w: upgrade oh"                                                         // when the config format is newer than this oh reads
	ConfigTooLarge       = "is larger than %s, which is more than a config is ever written by hand"        // after the file name, as in "oh.toml is larger than 1M, …"
	NotNormalFile        = "expected an ordinary file, not %s"                                             // as in "expected an ordinary file, not a named pipe"
	DirectoryKind        = "a directory"                                                                   // in NotNormalFile
	NamedPipeKind        = "a named pipe"                                                                  // in NotNormalFile
	SocketKind           = "a socket"                                                                      // in NotNormalFile
	DeviceKind           = "a device"                                                                      // in NotNormalFile
	OtherFileKind        = "a special file"                                                                // in NotNormalFile for any other kind
	NotDirectory         = "%s is not a directory"                                                         // when a config directory is a file
	WatchFailure         = "could not watch the config"                                                    // before the reason that the config cannot be watched
	WatchStopped         = "inotify stopped unexpectedly"                                                  // when the config watch ends by itself
	FileReadFailure      = "could not read %s"                                                             // before the reason that a file cannot be read
	PathExpandFailure    = "could not expand %q"                                                           // before the reason that a path cannot be expanded
	SizeShape            = "%q is not a size; write a count of bytes, or one with a K, M or G after it"    // when a size is not a number
	SizeTooLarge         = "%q is larger than this machine can count"                                      // when a size overflows
	ToolOutputTooSmall   = "%s is too small to say anything with; write at least %d bytes"                 // as in "default.tool_output is too small to say anything with; …"
	HomeGrantOutside     = "%s is not below the home directory, so it has nowhere to land"                 // as in "/srv is not below the home directory, so it has nowhere to land"
	PriorityChoice       = "priority must be %s or %s"                                                     // as in "priority must be normal or important"
	CurrencyEmpty        = "%s is empty; leave it out to count in dollars"                                 // as in "ui.currency is empty; …"
	EditorCommandEmpty   = "%s is empty; leave it out to find an editor automatically"                     // as in "editor.command is empty; …"
	HostnameEmpty        = "%s is empty; leave it out to use the numeric address"                          // as in "experimental.forward_hostname is empty; …"
	HostnamePlaceholder  = "%s must contain %s exactly once"                                               // as in "experimental.forward_hostname must contain {session} exactly once"
	HostnameTooLong      = "%s is %d characters long, and a hostname is at most %d"                        // when the forward hostname is too long
	HostnameCharacter    = "%s holds %q, and a hostname is letters, digits, dots and dashes"               // when the forward hostname holds another character
	SelectionNotText     = "model selection %d is not text"                                                // when a round_robin entry is not text, as in "model selection 2 is not a string"
	RoundRobinShape      = "%s is not a path or array of model selections"                                 // as in "round_robin is not a path or array of model selections"
	RoundRobinEmpty      = "%s is empty, so there is nothing to ask"                                       // as in "agent.round_robin is empty, so there is nothing to ask"
	RoundRobinFileEmpty  = "the file contains no model selections, so there is nothing to ask"             // when a round_robin file holds no selections
	EmptySelection       = "%s contains an empty selection"                                                // as in "agent.round_robin contains an empty selection"
	EmptyEntry           = "%s contains an empty entry"                                                    // as in "input.speed_dial contains an empty entry"
	PermissionShape      = "%s is not a rule or a table"                                                   // as in "permission is not a rule or a table"
	TimeoutOnlyWhenRule  = "%s applies only when %s is %q"                                                 // as in `timeout applies only when rule is "ask"`
	UngroupedDenial      = "%s has no effect without a group; add a group or remove the setting"           // as in "granted = false has no effect without a group; …"
	GroupLetter          = "group %q is not one lowercase letter"                                          // when a custom tool group is not a letter
	OnlyForBash          = "%s is only valid for bash"                                                     // as in "tool.read.host_network is only valid for bash"
	ReservedParameter    = "parameter %s is reserved for task execution"                                   // when a backgroundable tool names a reserved parameter
	BuiltinToolSetting   = "%s cannot be set on a built-in tool"                                           // as in "command cannot be set on a built-in tool"
)

// The report that oh --ctl analyse prints. Each section has a heading and a table. The tables also
// take ProviderColumn, ModelColumn, InputColumn and OutputColumn. A note can follow a table.
const (
	CacheSection                = "Cache"                                                                                                   // heads a section of the report
	TokensSection               = "Tokens"                                                                                                  // heads a section of the report
	ModelsSection               = "Models"                                                                                                  // heads a section of the report
	ConversationSection         = "Conversation"                                                                                            // heads a section of the report
	FaultsSection               = "Faults"                                                                                                  // heads a section of the report
	ToolsSection                = "Tools"                                                                                                   // heads a section of the report
	SessionsSection             = "Sessions"                                                                                                // heads a section of the report
	TotalRow                    = "Total"                                                                                                   // names the row that sums every provider
	NothingRecorded             = "Nothing was recorded."                                                                                   // instead of the report when no session has data
	SessionsColumn              = "Sessions"                                                                                                // heads the count of sessions
	RequestsColumn              = "Requests"                                                                                                // heads the count of requests
	HitsColumn                  = "Hits"                                                                                                    // heads the count of cache hits
	MissesColumn                = "Misses"                                                                                                  // heads the count of cache misses
	HitRateColumn               = "Hit rate"                                                                                                // heads the share of requests that hit the cache
	InputCachedColumn           = "Input cached"                                                                                            // heads the share of input read from the cache
	TokensReadColumn            = "Tokens read"                                                                                             // heads the count of tokens read from the cache
	AverageInputColumn          = "Average input"                                                                                           // heads the mean input of a request
	PeakInputColumn             = "Peak input"                                                                                              // heads the largest input of a request
	TotalInputColumn            = "Total input"                                                                                             // heads the sum of all input
	CacheReadsColumn            = "Cache reads"                                                                                             // heads the tokens read from the cache
	CacheWritesColumn           = "Cache writes"                                                                                            // heads the tokens written to the cache
	FreshInputColumn            = "Fresh input"                                                                                             // heads the input that missed the cache
	SpendColumn                 = "Spend"                                                                                                   // heads the money spent
	TurnsColumn                 = "Turns"                                                                                                   // heads the count of turns
	PromptsColumn               = "Prompts"                                                                                                 // heads the count of prompts
	RepliesColumn               = "Replies"                                                                                                 // heads the count of replies
	ReasoningColumn             = "Reasoning"                                                                                               // heads the count of reasoning blocks
	ToolCallsColumn             = "Tool calls"                                                                                              // heads the count of tool calls
	AverageTurnColumn           = "Average turn"                                                                                            // heads the mean length of a turn
	LongestTurnColumn           = "Longest turn"                                                                                            // heads the length of the longest turn
	OpenForColumn               = "Open for"                                                                                                // heads the time a session was open
	RetriesColumn               = "Retries"                                                                                                 // heads the count of retries
	InterruptionsColumn         = "Interruptions"                                                                                           // heads the count of interruptions
	FailuresColumn              = "Failures"                                                                                                // heads the count of failures
	CompressionRejectionsColumn = "Compression rejections"                                                                                  // heads the count of refused compressed requests
	SilentTurnsColumn           = "Silent turns"                                                                                            // heads the count of turns with no reply
	RewritesColumn              = "Rewrites"                                                                                                // heads the count of prefix rewrites
	RebuildsColumn              = "Rebuilds"                                                                                                // heads the count of cache rebuilds
	RebuiltTokensColumn         = "Rebuilt tokens"                                                                                          // heads the tokens that cache rebuilds wrote again
	ToolColumn                  = "Tool"                                                                                                    // heads the column of tool names
	CallsColumn                 = "Calls"                                                                                                   // heads the count of calls
	CancelledColumn             = "Cancelled"                                                                                               // heads the count of cancelled calls
	FailureRateColumn           = "Failure rate"                                                                                            // heads the share of calls that failed
	TotalTimeColumn             = "Total time"                                                                                              // heads the sum of call times
	AverageCallColumn           = "Average call"                                                                                            // heads the mean time of a call
	SessionColumn               = "Session"                                                                                                 // heads the column of session names
	MissingSubagentJournal      = "%s subagent's journal is gone, so only its usage and spend are counted, from its parent's record."       // when one subagent journal is gone, as in "1 subagent's journal is gone, …"
	MissingSubagentJournals     = "%s subagents' journals are gone, so only their usage and spend are counted, from their parent's record." // when more than one subagent journal is gone
	UnpricedModel               = "%s model has no listed price, so its spend is unknown and missing from the total."                       // when one model has no price
	UnpricedModels              = "%s models have no listed price, so their spend is unknown and missing from the total."                   // when more than one model has no price
	LegacySession               = "%s of %s session is in a legacy format and was not considered."                                          // when the only session is in a legacy format, as in "1 of 1 session is …"
	LegacySessionOfMany         = "%s of %s sessions is in a legacy format and was not considered."                                         // when one of several sessions is in a legacy format
	LegacySessions              = "%s of %s sessions are in a legacy format and were not considered."                                       // when several sessions are in a legacy format
	ArchivedAnalysis            = "the session %q is archived"                                                                              // refuses to analyse an archived session
	NoStoredSessionNamed        = "there is no stored session named %q"                                                                     // refuses to analyse an unknown session
	AnalysisFailure             = "could not analyse %s"                                                                                    // before the reason that a journal cannot be analysed
)

// The report that oh --ctl gc prints. The summary joins its parts with commas, as in "3 paths, 12M
// reclaimed, 1 running session and the shared home left alone, also 2 empty directories".
const (
	KindColumn         = "Kind"                              // heads the column of kinds of removed path
	PathColumn         = "Path"                              // heads the column of removed paths
	PathNoun           = "path"                              // the count of removed paths, as in "3 paths", with "s" added for more than one
	ReclaimedSummary   = "%s, %s reclaimed"                  // as in "3 paths, 12M reclaimed"
	ReclaimableSummary = "%s, %s to reclaim"                 // with --dry-run, as in "3 paths, 12M to reclaim"
	RunningSessionNoun = "running session"                   // the count of running sessions, with "s" added for more than one
	LeftAlone          = "%s and the shared home left alone" // as in "1 running session and the shared home left alone"
	EmptyDirectory     = "also %d empty directory"           // when one empty directory goes
	EmptyDirectories   = "also %d empty directories"         // when more than one empty directory goes
	RemovalFailures    = "%d of %d could not be removed"     // as in "2 of 9 could not be removed"
)

// The report that oh --ctl check prints. Each row starts with a status, then the target and the
// detail, as in "fixed wiry-turtle: rebuild the transcript".
const (
	CheckPassed             = "ok"                                                                         // the status of a healthy target
	CheckFixed              = "fixed"                                                                      // the status of a repaired target
	CheckWarning            = "warning"                                                                    // the status of a target with a recoverable problem
	CheckRepairable         = "repairable"                                                                 // the status of a target that --fix can repair
	CheckPlanned            = "would fix"                                                                  // the status of a target with --dry-run
	CheckFailed             = "failed"                                                                     // the status of a broken target
	ConfigTarget            = "config"                                                                     // names the config files
	StoreTarget             = "session store"                                                              // names the session store
	EmptyStore              = "session store (no sessions)"                                                // after ok when no session exists
	SessionNoun             = "session"                                                                    // the count of sessions, with "s" added for more than one
	FixedCount              = "%d of %s"                                                                   // after fixed, as in "fixed 2 of 9 sessions"
	CheckedCount            = "checked %s"                                                                 // when some sessions failed, as in "checked 9 sessions"
	HealthCheckNoun         = "health check"                                                               // the count of failed checks, with "s" added for more than one
	ChecksFailed            = "%s failed"                                                                  // as in "2 health checks failed"
	SkippedInUse            = "skipped %s in use"                                                          // when a session is running
	RecoverableTurn         = "unfinished turn is recoverable"                                             // a warning about a turn that a resume recovers
	RestoreBeforeFixing     = "restore the archived session before fixing it"                              // when --fix meets an archived session
	UnrecognisedEntry       = "unrecognised entry in the session store"                                    // when the store holds an unknown file
	RemoveIncompleteRecord  = "remove an incomplete final journal record"                                  // a repair
	AbandonTurn             = "abandon an unfinished turn"                                                 // a repair
	RebuildMetadata         = "rebuild listing metadata"                                                   // a repair
	RebuildTranscript       = "rebuild the transcript"                                                     // a repair
	CopyKept                = "; copy kept in %s"                                                          // after the repairs of a fixed session
	FlagNeedsFlag           = "%s requires %s"                                                             // as in "--dry-run requires --fix"
	CopyFailure             = "keep a copy"                                                                // before the reason that the copy failed
	OpenedDuringRepair      = "the session was opened while it was being repaired"                         // when a session starts during a repair
	RepairLeft              = "repair left %s"                                                             // when repairs remain, as in "repair left rebuild the transcript"
	ChangedDuringRepair     = "the session changed as its repair started; no further repair was attempted" // when a session changes as a repair starts
	CopyAlreadyKept         = "a copy is already kept in %s: move it aside first"                          // when an earlier copy is in the way
	NeitherFileNorDirectory = "%s is neither a file nor a directory"                                       // when a copy meets an odd entry
)

// The preview of a conversation in a picker. Each action follows its keys in the key line, as in
// "esc/q return". The position shows as "1-20 of 64".
const (
	PreviewReturnAction  = "return"                        // after esc/q, which closes the preview
	PreviewScrollAction  = "scroll"                        // after ↑↓
	PreviewBrowseAction  = "browse"                        // after ←→, which moves to the next or previous session
	PreviewOpenAction    = "to open"                       // after enter, as in "enter to open"
	PreviewRunningStatus = "running"                       // starts the key line of a preview of a running session
	PreviewLoadPrompt    = "Press %s to load…"             // in the input row of a preview, as in "Press enter to load…"
	PreviewRestorePrompt = "Press %s to restore and load…" // in the input row of a preview of an archived session
	PreviewRunningPrompt = "This session is running…"      // in the input row of a preview of a running session
	PreviewPosition      = "%d-%d of %d"                   // the lines that show, as in "1-20 of 64"
)

// The first run, which greets the person and signs in to a provider. A note follows the name of
// each provider in the list, as in "Anthropic  OAuth". The key prompt names its provider.
const (
	Greeting             = "Oh, hello."                                                                                       // the first line, typed out
	GreetingAside        = "*yawns*"                                                                                          // in italics after Greeting
	Introduction         = "Yes? Oh, right. Let's get you signed in."                                                         // the second line, typed out
	Farewell             = "Thanks. Transferring you…"                                                                        // after the setup, before the session opens
	ProviderPrompt       = "Choose your provider:"                                                                            // above the list of providers
	ModelPrompt          = "Choose a model:"                                                                                  // above the list of models of the chosen provider
	KeyEntryPrompt       = "%s API key: "                                                                                     // asks for a key, as in "OpenCode Go API key: "
	EmptyKeyEntry        = "%s API key is empty"                                                                              // refuses an empty key, as in "OpenCode Go API key is empty"
	PasteHint            = "If the redirect breaks, paste the full redirect URL here instead."                                // below the sign-in URL, for when the redirect fails
	BrowserHint          = "Visit the URL above to continue."                                                                 // below the sign-in URL, when no browser opens
	OpeningBrowser       = "Opening your browser to shake hands with %s…"                                                     // before the browser opens, as in "Opening your browser to shake hands with Anthropic…"
	ProviderActionPrompt = "Choose what to do with %s:"                                                                       // above the actions for a provider with a sign-in or a key, as in "Choose what to do with Anthropic:"
	DoNothing            = "Nothing"                                                                                          // the last action, which changes nothing
	OAuthNote            = "OAuth"                                                                                            // beside a provider that signs in with OAuth
	KeyNote              = "Key"                                                                                              // beside a provider that takes an API key
	SimulatorNote        = "The doctor will see you now"                                                                      // beside the simulator
	NoPrintModel         = "there is no model to print with yet; choose one with -m, or run oh once without -p to set one up" // refuses oh -p when no model is set
	NoModelsAvailable    = "no models are available for %s"                                                                   // when the chosen provider lists no models, as in "no models are available for Anthropic"
	UnknownProviderName  = "unknown provider %q: choose one of %s"                                                            // refuses an unknown name given to oh --login, as in `unknown provider "foo": choose one of codex, opencode-go, anthropic`
)

// A provider that signs in with OAuth. The presence follows the note in the list of providers, as
// in "OAuth · signed in". The renewal and the removal are the actions for a signed-in provider.
const (
	SignInPresence      = "signed in"                        // beside a signed-in provider
	SignInRenewal       = "Sign in again"                    // the action that signs in again
	SignInRemoval       = "Sign out"                         // the action that signs out
	SignInAddition      = "Signed in to %s"                  // after a sign-in, as in "Signed in to Anthropic"
	SignInWithdrawal    = "Signed out of %s"                 // after a sign-out, as in "Signed out of Anthropic"
	SignInFailure       = "Could not sign in: %s"            // after a failed sign-in, with the reason
	SignInChangeFailure = "Could not update the sign-in: %s" // after a failed sign-in again or sign-out, with the reason
)

// A provider that takes an API key. The presence follows the note, as in "Key · saved".
const (
	KeyPresence      = "saved"                        // beside a provider with a saved key
	KeyRenewal       = "Replace key"                  // the action that replaces the key
	KeyRemoval       = "Remove key"                   // the action that removes the key
	KeyAddition      = "Saved the %s key"             // after a key is saved, as in "Saved the OpenCode Go key"
	KeyWithdrawal    = "Removed the %s key"           // after a key is removed, as in "Removed the OpenCode Go key"
	KeyFailure       = "Could not save the key: %s"   // after a key is refused, with the reason
	KeyChangeFailure = "Could not update the key: %s" // after a failed key replacement or removal, with the reason
)

// The approval question for a tool or a command. The lapse comes before the time that remains, as
// in "denies in 1m". The options show from left to right in this order.
const (
	HostNetworkQuestion   = "Run in the sandbox on the host network?" // above a command that asks for the host network
	LookupQuestion        = "Look this up on the web?"                // above a web lookup
	FetchQuestion         = "Fetch this page?"                        // above the address of a page to fetch
	CustomToolQuestion    = "Run the %s tool?"                        // above the arguments of a custom tool, as in "Run the commit tool?"
	ApprovalLapse         = "denies"                                  // before the time left, as in "denies in 1m"
	DenyOption            = "Deny"                                    // the first option
	AllowOnceOption       = "Allow"                                   // the second option, and the default
	AllowForMinutesOption = "Allow for 5m"                            // the third option, which approves for 5 minutes
	AllowForSessionOption = "Allow for session"                       // the fourth option, which approves until oh quits
)

// The external editor that ^G and ^. open. A lead comes before the name of the editor, as in
// "Editing the draft in vim". A hint names its key, as in "esc to keep the draft as it was".
const (
	EditingDraftLead = "Editing the draft in "            // while ^G edits the draft
	WritingReplyLead = "Writing the reply in "            // while ^. writes a reply
	KeepDraftHint    = "%s to keep the draft as it was"   // at the right of EditingDraftLead, as in "esc to keep the draft as it was"
	DropReplyHint    = "%s to drop the reply"             // at the right of WritingReplyLead, as in "esc to drop the reply"
	DraftKept        = "The draft was kept as it was: %s" // when the editor fails on the draft, with the reason
	ReplyDropped     = "The reply was dropped: %s"        // when the editor fails on a reply, with the reason
	EditorFailure    = "The editor failed: %s"            // when the editor fails, with the reason
	PagerFailure     = "The pager failed: %s"             // when the pager for /sub fails, with the reason
	DraftEditFailure = "Could not edit the draft: %s"     // when ^G cannot open the editor, with the reason
	ReplyFailure     = "Could not write the reply: %s"    // when ^. cannot open the editor, with the reason
)

// The failures that the feedback row shows. The reason comes last, as in "Could not run the
// command: sandbox is unavailable".
const (
	HostCommandFailure         = "Could not run the command: %s"                   // when a /! command cannot start
	ExchangeRateFailure        = "Could not refresh the exchange rate for %s: %s"  // as in "Could not refresh the exchange rate for EUR: timeout"
	StateRestoreFailure        = "Could not restore the session state: %s"         // when a resumed session cannot restore its agent state
	ConversationRestoreFailure = "Could not restore the conversation: %s"          // when a resumed session cannot load its conversation
	PathGrantCarryFailure      = "Could not carry over temporary access to %s: %v" // when a fork cannot keep a /grant, as in "Could not carry over temporary access to /srv: no such file"
	PathGrantRestoreFailure    = "Could not restore temporary access to %s: %v"    // when a resumed session cannot open a /grant again
	PortForwardFailure         = "Could not forward port %d again: %v"             // when a fork or a resumed session cannot open a /forward again, as in "Could not forward port 8080 again: address already in use"
	ConversationStoreFailure   = "Could not store the conversation: %s"            // when an event cannot be written to the journal
	TurnStoreFailure           = "Could not store the end of the turn: %s"         // when the end of a turn cannot be written to the journal
	ConversationStateFailure   = "Could not store the conversation state: %s"      // when the provider state cannot be written to the journal
)

// The feedback for the stash that ^S keeps, and for the redraw that ^L does, as in "Redrawn in
// 42ms".
const (
	StashFailure        = "Could not stash the draft: %s"           // when ^S cannot stash the draft, with the reason
	StashRestoreFailure = "Could not restore the stashed draft: %s" // when ^S cannot restore a stashed draft, with the reason
	StashEmpty          = "The stash is empty."                     // when ^Shift+S finds nothing to swap
	RedrawTime          = "Redrawn in %s"                           // after ^L, with the redraw-timing experiment on
)

// The clipboard paste. The terminal shows the event name when it asks to share the clipboard. A
// reason follows PasteFailure, as in "Could not paste: another window is using the clipboard".
const (
	PasteEventName    = "Paste event"                                  // the name in the terminal's request to read the clipboard
	PasteFailure      = "Could not paste: %s"                          // when a paste fails, with the reason
	ClipboardTooLarge = "the clipboard holds more than %s"             // refuses a clipboard over the limit, as in "the clipboard holds more than 32M"
	ClipboardRefused  = "permission to read the clipboard was refused" // when the terminal answers EPERM
	ClipboardMissing  = "the terminal has no clipboard to read"        // when the terminal answers ENOSYS
	ClipboardBusy     = "another window is using the clipboard"        // when the terminal answers EBUSY
	ClipboardFailure  = "the terminal refused the clipboard with %s"   // when the terminal answers another code, as in "the terminal refused the clipboard with EIO"
)
