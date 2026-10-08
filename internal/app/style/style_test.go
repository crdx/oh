package style

import (
	"errors"
	"strings"
	"testing"
)

func enableColor(t *testing.T) {
	t.Helper()

	previous := isColorEnabled
	apply(true)

	t.Cleanup(func() { apply(previous) })
}

func TestAnErrorIsMarkedCapitalisedAndIndented(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	got := Error(errors.New("first failure\nsecond failure"))
	want := "✗ First failure\n  Second failure"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnErrorUsesTheFailureStyle(t *testing.T) {
	enableColor(t)

	got := Error(errors.New("failed"))
	if Plain(got) != "✗ Failed" {
		t.Errorf("got %q", got)
	}
	if got == Plain(got) {
		t.Errorf("the error was not styled: %q", got)
	}
}

func TestAWarningIsPrefixedAndUsesTheWarningStyle(t *testing.T) {
	enableColor(t)

	var output strings.Builder
	WriteWarningf(&output, "%s is %d", "answer", 42)

	got := output.String()
	if Plain(got) != "warning: answer is 42\n" {
		t.Errorf("got %q", got)
	}
	if got != Warning("warning: answer is 42")+"\n" {
		t.Errorf("the warning was not styled: %q", got)
	}
}

func TestApplyingAThemeChangesExistingStyles(t *testing.T) {
	enableColor(t)

	theme := DefaultTheme()
	theme.Dark.Accent = "#010203"
	theme.Dark.User = "#040506"
	theme.Dark.Harness = "#070809"
	t.Cleanup(ApplyTheme(theme))

	if got := Subject("subject"); !strings.Contains(got, "38;2;1;2;3") {
		t.Errorf("accent used the wrong theme colour: %q", got)
	}
	if got := User("user"); !strings.Contains(got, "48;2;4;5;6") {
		t.Errorf("user message used the wrong theme background: %q", got)
	}
	if got := Harness("harness"); !strings.Contains(got, "48;2;7;8;9") {
		t.Errorf("harness message used the wrong theme background: %q", got)
	}
}

func TestEveryStyleFollowsItsOwnPaletteRole(t *testing.T) {
	enableColor(t)

	t.Cleanup(ApplyTheme(Theme{Dark: Palette{
		Normal:         "#010101",
		Dim:            "#020202",
		Accent:         "#030303",
		StatusSuccess:  "#040404",
		StatusInfo:     "#050505",
		StatusWarning:  "#060606",
		StatusDanger:   "#070707",
		SyntaxType:     "#080808",
		SyntaxLiteral:  "#090909",
		SyntaxOperator: "#0a0a0a",
		SyntaxKeyword:  "#0b0b0b",
		Skill:          "#0c0c0c",
	}}))

	roles := map[string]map[string]Style{
		"38;2;1;1;1": {
			"answer":      Answer,
			"call":        Call,
			"normal":      Normal,
			"punctuation": Punctuation,
			"typed input": TypedInput,
			"variable":    Variable,
		},
		"38;2;2;2;2": {
			"address":        Address,
			"block":          Block,
			"border":         Border,
			"cancelled call": CancelledCall,
			"comment":        Comment,
			"dim":            Dim,
			"qualifier":      Qualifier,
			"quote":          Quote,
			"result":         Result,
			"rule":           Rule,
			"scrolled input": ScrolledInput,
			"subtle":         Subtle,
		},
		"38;2;3;3;3": {
			"accent":     Accent,
			"bullet":     Bullet,
			"chosen row": ChosenRow,
			"code":       Code,
			"number":     Number,
			"prompt":     Prompt,
			"spinner":    Spinner,
			"subject":    Subject,
		},
		"38;2;4;4;4": {
			"clean tree":        CleanTree,
			"inserted text":     InsertedText,
			"low price":         LowPrice,
			"preview load hint": PreviewLoadHint,
			"read":              Read,
			"success":           Success,
		},
		"38;2;5;5;5": {
			"divergence":    Divergence,
			"function":      Function,
			"hunk":          Hunk,
			"information":   Info,
			"link":          Link,
			"lookup":        Lookup,
			"medium price":  MediumPrice,
			"network":       Network,
			"scratch alias": ScratchAlias,
			"shell":         Shell,
		},
		"38;2;6;6;6": {
			"change":               Change,
			"dirty tree":           DirtyTree,
			"heading":              Heading,
			"high price":           HighPrice,
			"preview running hint": PreviewRunningHint,
			"stopped turn":         StoppedTurn,
			"warning":              Warning,
			"write":                Write,
		},
		"38;2;7;7;7": {
			"deleted text":  DeletedText,
			"extreme price": WtfPrice,
			"failure":       Failure,
			"hazard":        Hazard,
			"troubled tree": TroubledTree,
		},
		"38;2;8;8;8":    {"type": Type},
		"38;2;9;9;9":    {"literal": Literal},
		"38;2;10;10;10": {"operator": Operator},
		"38;2;11;11;11": {"keyword": Keyword},
		"38;2;12;12;12": {"skill": Skill},
	}

	for sequence, styles := range roles {
		for name, paint := range styles {
			if got := paint("text"); !strings.Contains(got, sequence) {
				t.Errorf("%s did not follow its palette role: %q", name, got)
			}
		}
	}
}

func TestAThemeCanUseTheTerminalDefault(t *testing.T) {
	enableColor(t)

	theme := DefaultTheme()
	theme.Dark.Accent = ""
	t.Cleanup(ApplyTheme(theme))

	if got := Subject("subject"); got != "subject" {
		t.Errorf("terminal-default accent was painted %q", got)
	}
}

func TestDisabledCapabilitiesTakeTheMutedColour(t *testing.T) {
	enableColor(t)

	got := Dim("w")

	if strings.Contains(got, "\x1b[2m") {
		t.Errorf("expected no reduced intensity on a disabled capability, got %q", got)
	}

	if !strings.Contains(got, "\x1b[38;2;") {
		t.Errorf("expected the muted colour, got %q", got)
	}
}

func TestAToolAppearanceCanConfigureItsNameAndPaints(t *testing.T) {
	theme := DefaultTheme()
	theme.Tool["skill"] = ToolEntry{Default: ToolAppearance{
		Name:  "consult-chart",
		Paint: "#010203 bold",
		Focus: "status_danger",
	}}
	t.Cleanup(ApplyTheme(theme))

	name, nameStyle, focusStyle := ToolCallAppearance("skill", "read", true)
	if name != "consult-chart" {
		t.Errorf("got name %q", name)
	}
	if got := nameStyle("name"); got != "\x1b[1;38;2;1;2;3mname\x1b[0m" {
		t.Errorf("got name paint %q", got)
	}
	if got, want := focusStyle("focus"), Failure("focus"); got != want {
		t.Errorf("got focus paint %q, want %q", got, want)
	}
}

func TestAnUnknownToolAppearanceUsesItsCallDefaults(t *testing.T) {
	name, readStyle, _ := ToolCallAppearance("custom", "inspect", true)
	if name != "inspect" || readStyle("call") != Call("call") {
		t.Errorf("got %q and %q", name, readStyle("call"))
	}
	_, changeStyle, _ := ToolCallAppearance("custom", "change", false)
	if changeStyle("call") != Change("call") {
		t.Errorf("got change style %q", changeStyle("call"))
	}
}

func TestObservationalActionsUseNormalPaintWithinToolsThatMayChangeThings(t *testing.T) {
	enableColor(t)

	for _, kind := range []string{
		"job_status",
		"job_output",
		"job_wait_any",
		"job_wait_all",
		"job_list",
		"forward_list",
	} {
		_, nameStyle, _ := ToolCallAppearance(kind, "", false)
		if got, want := nameStyle("call"), Normal("call"); got != want {
			t.Errorf("%s used %q, want normal paint %q", kind, got, want)
		}
	}
}

func TestTheShellPromptMatchesACommandName(t *testing.T) {
	enableColor(t)

	if got, want := Shell("$"), Function("$"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAUserMessageHasABackgroundThatSurvivesInnerStyles(t *testing.T) {
	enableColor(t)

	got := User("before " + Code("inside") + " after")

	if count := strings.Count(got, "\x1b[48;2;"); count < 2 {
		t.Errorf("expected the background to resume after the inner style, got %q", got)
	}

	if plain := Plain(got); plain != "before inside after" {
		t.Errorf("expected the message text unchanged, got %q", plain)
	}
}

func TestAStyleOverAnotherResumesWhereTheInnerOneReset(t *testing.T) {
	enableColor(t)

	opening := "\x1b[" + foregroundSequence(DefaultTheme().Dark.Accent) + "m"
	got := ChosenRow.Over("row " + Qualifier("note") + " tail")

	if count := strings.Count(got, opening); count != 2 {
		t.Errorf("expected the outer style to resume after the inner one, got %q", got)
	}

	if strings.HasSuffix(got, opening+reset) {
		t.Errorf("expected nothing opened at the very end, got %q", got)
	}

	if plain := Plain(got); plain != "row note tail" {
		t.Errorf("expected the row's text unchanged, got %q", plain)
	}
}

func TestAStyleOverAnotherPaintsNothingWhereNothingIsPainted(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	if got := ChosenRow.Over("row note"); got != "row note" {
		t.Errorf("got %q, want the text left alone", got)
	}
}

func TestItalicTextStylesAreItalic(t *testing.T) {
	enableColor(t)

	for name, paint := range map[string]Style{
		"preview load hint":    PreviewLoadHint,
		"preview running hint": PreviewRunningHint,
		"reasoning":            Reasoning,
		"running session":      RunningSession,
		"scratch alias":        ScratchAlias,
	} {
		got := paint("looking %s", "here")
		if !strings.Contains(got, "\x1b[3m") {
			t.Errorf("expected italic %s, got %q", name, got)
		}
		if plain := Plain(got); plain != "looking here" {
			t.Errorf("expected the %s text unchanged, got %q", name, plain)
		}
	}
}

func TestPreviewHintsAreDim(t *testing.T) {
	enableColor(t)

	for name, paint := range map[string]Style{
		"load":    PreviewLoadHint,
		"running": PreviewRunningHint,
	} {
		if got := paint("hint"); !strings.Contains(got, "\x1b[2m") {
			t.Errorf("expected dim %s hint, got %q", name, got)
		}
	}
}

func TestPlainKeepsTextCarriedByTheTextSizingProtocol(t *testing.T) {
	sized := "\x1b]66;s=2:w=2;🐟\x1b\\"
	if got := Plain("before " + sized + " after"); got != "before 🐟 after" {
		t.Errorf("got %q, want the visible text", got)
	}
	if got := Width(sized); got != 4 {
		t.Errorf("width = %d, want 4", got)
	}
}

func TestNothingIsPaintedWhereTheScreenIsNotATerminal(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	if isColorEnabled {
		t.Fatal("expected colour to be off when the screen is not a terminal")
	}

	for name, paint := range map[string]Style{
		"failure":   Failure,
		"subject":   Subject,
		"reasoning": Reasoning,
		"user":      User,
	} {
		if got := paint("hello"); got != "hello" {
			t.Errorf("%s painted %q, want it left alone", name, got)
		}
	}

	if got := PendingPrefix(Read("r")); got != "r" {
		t.Errorf("a style over another painted %q, want it left alone", got)
	}
}

func TestInitPutsTheDecisionBackWhenItsRestoreIsCalled(t *testing.T) {
	enableColor(t)

	restore := Init(&strings.Builder{})

	if isColorEnabled {
		t.Fatal("expected colour off while the screen is not a terminal")
	}

	restore()

	if !isColorEnabled {
		t.Fatal("expected colour back on once the decision was put back")
	}

	if got := Failure("hello"); got == "hello" {
		t.Errorf("expected painting to resume, got %q", got)
	}
}

func TestJoinDropsEmptyPartsAndSpacesTheRest(t *testing.T) {
	enableColor(t)

	if got, want := Subtle.Join("4L", "", "~2k"), Subtle("4L ~2k"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinPaintsNothingWhenEveryPartIsEmpty(t *testing.T) {
	enableColor(t)

	if got := Subtle.Join("", ""); got != "" {
		t.Errorf("got %q, want an empty string", got)
	}
}

func TestAGradientPaintsEveryCharacterOnItsWayFromOneColourToTheOther(t *testing.T) {
	enableColor(t)

	painted := Simulation("Simulation")

	if got := Plain(painted); got != "Simulation" {
		t.Errorf("the gradient drew %q", got)
	}
	if !strings.Contains(painted, "\x1b["+italicCode+";") {
		t.Errorf("the gradient drew no italics: %q", painted)
	}

	gradient := simulationGradients[DarkBackground]
	if !strings.Contains(painted, sequenceFor(gradient.from)) {
		t.Errorf("the gradient does not start at %v: %q", gradient.from, painted)
	}
	if !strings.Contains(painted, sequenceFor(gradient.to)) {
		t.Errorf("the gradient does not end at %v: %q", gradient.to, painted)
	}
	if Width(painted) != len("Simulation") {
		t.Errorf("the gradient measures %d cells", Width(painted))
	}
}

func TestAGradientLeavesTheWordsAloneWhereColourIsOff(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	if got := Simulation("Simulation"); got != "Simulation" {
		t.Errorf("a colourless gradient drew %q", got)
	}
}

func TestEveryForwardKindHasItsOwnDefaultNameAndTheRetiredExposeKindsHaveNone(t *testing.T) {
	for kind, want := range map[string]string{
		"forward":        "forward",
		"forward_add":    "forward",
		"forward_remove": "close",
		"forward_list":   "list",
	} {
		if name, _, _ := ToolCallAppearance(kind, "recorded", false); name != want {
			t.Errorf("%s is named %q, want %q", kind, name, want)
		}
	}

	for _, kind := range []string{"expose", "expose_add", "expose_remove", "expose_list"} {
		if name, _, _ := ToolCallAppearance(kind, "recorded", false); name != "recorded" {
			t.Errorf("the retired %s kind is still themed as %q", kind, name)
		}
	}
}

func TestAnActionInheritsItsToolsPaintsAndIsNamedAfterItself(t *testing.T) {
	enableColor(t)

	theme := DefaultTheme()
	theme.Tool["tool"] = ToolEntry{
		Default: ToolAppearance{Name: "whole", Paint: "status_danger", Focus: "status_info"},
		Actions: map[string]ToolAppearance{
			"go":   {},
			"stop": {Name: "halt"},
			"look": {Paint: "normal"},
		},
	}
	t.Cleanup(ApplyTheme(theme))

	for kind, want := range map[string]struct {
		name  string
		paint Style
		focus Style
	}{
		"tool":      {name: "whole", paint: Failure, focus: Info},
		"tool_go":   {name: "go", paint: Failure, focus: Info},
		"tool_stop": {name: "halt", paint: Failure, focus: Info},
		"tool_look": {name: "look", paint: Normal, focus: Info},
	} {
		name, nameStyle, focusStyle := ToolCallAppearance(kind, "recorded", false)
		if name != want.name {
			t.Errorf("%s is named %q, want %q", kind, name, want.name)
		}
		if got := nameStyle("x"); got != want.paint("x") {
			t.Errorf("%s is painted %q, want %q", kind, got, want.paint("x"))
		}
		if got := focusStyle("x"); got != want.focus("x") {
			t.Errorf("%s focuses with %q, want %q", kind, got, want.focus("x"))
		}
	}
}

func TestNoBuiltInToolHasAnActionNamedLikeItsDefault(t *testing.T) {
	for tool, entry := range DefaultTheme().Tool {
		for action := range entry.Actions {
			switch action {
			case toolDefaultKey, toolNameKey, toolPaintKey, toolFocusKey:
				t.Errorf("%s has an action called %q, which its theme cannot tell from its own appearance", tool, action)
			}
		}
	}
}

func TestAThemeCannotBeChangedThroughAClone(t *testing.T) {
	theme := DefaultTheme()
	theme.Tool["job"].Actions["start"] = ToolAppearance{Name: "launch"}

	if got := DefaultTheme().Tool["job"].Actions["start"].Name; got != "" {
		t.Errorf("a change to a copy reached the built-in theme: %q", got)
	}
}

func TestEveryBuiltInToolKindKeepsItsNameAndPaint(t *testing.T) {
	want := map[string]ToolAppearance{
		"read":              {Name: "read"},
		"skill":             {Name: "load", Paint: "skill", Focus: "skill"},
		"bash":              {Name: "$", Paint: "status_info"},
		"bash_host_network": {Name: "$", Paint: "status_danger"},
		"job":               {Name: "job", Paint: "status_warning"},
		"job_start":         {Name: "start", Paint: "status_warning"},
		"job_stop":          {Name: "stop", Paint: "status_warning"},
		"job_restart":       {Name: "restart", Paint: "status_warning"},
		"job_discard":       {Name: "discard", Paint: "status_warning"},
		"job_prune":         {Name: "prune", Paint: "status_warning"},
		"job_status":        {Name: "status", Paint: "normal"},
		"job_output":        {Name: "cat", Paint: "normal"},
		"job_wait_any":      {Name: "await", Paint: "normal"},
		"job_wait_all":      {Name: "await", Paint: "normal"},
		"job_list":          {Name: "list", Paint: "normal"},
		"forward":           {Name: "forward", Paint: "status_warning"},
		"forward_add":       {Name: "forward", Paint: "status_warning"},
		"forward_remove":    {Name: "close", Paint: "status_warning"},
		"forward_list":      {Name: "list", Paint: "normal"},
		"lookup":            {Name: "lookup", Paint: "status_info"},
	}

	theme := DefaultTheme()
	resolved := theme.Tool.Resolved()
	for kind, appearance := range want {
		if got := resolved[kind]; got != appearance {
			t.Errorf("%s resolves to %+v, want %+v", kind, got, appearance)
		}
	}
}
