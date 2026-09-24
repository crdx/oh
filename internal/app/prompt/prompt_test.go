package prompt

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/work"
)

func systemWorkspace(t *testing.T) *work.Space {
	t.Helper()

	workspace := work.At(t.TempDir())
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	return workspace
}

func configDir() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "org.crdx", "oh")
}

func globalContextPath() string {
	return filepath.Join(configDir(), globalContextName)
}

func loadTestContext(workspace *work.Space, skills []skill.Skill) (string, []File, error) {
	return Load(Config{
		GlobalPath:  globalContextPath(),
		Workspace:   workspace,
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
		Skills:      skills,
	})
}

func TestTheGlobalContextReplacesTheBuiltInOpeningButKeepsTheHarnessState(t *testing.T) {
	workspace := systemWorkspace(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredGlobalContext = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredGlobalContext), 0o600); err != nil {
		t.Fatal(err)
	}

	got, contextFiles, err := loadTestContext(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantcontextFiles := []File{{Name: "SYSTEM.md", Body: configuredGlobalContext}}
	if !slices.Equal(contextFiles, wantcontextFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantcontextFiles)
	}
	if !strings.Contains(got, "You are a deliberately custom assistant.") {
		t.Errorf("custom opening is missing: %q", got)
	}
	if strings.Contains(got, defaultGlobalContext) {
		t.Errorf("built-in opening was not replaced: %q", got)
	}
	if harness, opening := strings.Index(got, "# Scope"), strings.Index(got, "You are a deliberately"); harness > opening {
		t.Errorf("the harness does not come before the global context: %q", got)
	}
	for _, want := range []string{
		"The workspace (" + workspace.GetDir() + ") is read-only",
		"The workspace is not a git repository",
		"The bash tool is refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime prompt does not contain %q: %q", want, got)
		}
	}
}

func TestAMissingGlobalContextUsesTheBuiltInOpening(t *testing.T) {
	workspace := systemWorkspace(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadTestContext(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, defaultGlobalContext) {
		t.Errorf("built-in opening is missing: %q", got)
	}
	if harness, opening := strings.Index(got, "# Scope"), strings.Index(got, defaultGlobalContext); harness > opening {
		t.Errorf("the harness does not come before the built-in opening: %q", got)
	}
}

func TestContextcontextFilesFollowTheOrderTheyAreConcatenatedIn(t *testing.T) {
	workspace := systemWorkspace(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredGlobalContext = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredGlobalContext), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"AGENTS.md":       "Run the broad checks.",
		"AGENTS.local.md": "Never grant more access.",
	} {
		if err := os.WriteFile(filepath.Join(workspace.GetDir(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, contextFiles, err := loadTestContext(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantcontextFiles := []File{
		{Name: "SYSTEM.md", Body: configuredGlobalContext},
		{Name: "AGENTS.md", Body: "Run the broad checks."},
		{Name: "AGENTS.local.md", Body: "Never grant more access."},
	}
	if !slices.Equal(contextFiles, wantcontextFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantcontextFiles)
	}
	system := strings.Index(got, "You are a deliberately custom assistant.")
	agents := strings.Index(got, "Run the broad checks.")
	local := strings.Index(got, "Never grant more access.")
	if system == -1 || agents == -1 || local == -1 || system >= agents || agents >= local {
		t.Errorf("prompt files are absent or out of order: %q", got)
	}
}

func TestConfiguredPathsAreDisclosedInTheHarnessContext(t *testing.T) {
	paths := shell.Paths{
		Deny:  []string{"*.env"},
		Read:  []string{"/reference"},
		Write: []string{"/output"},
		Exec:  []string{"/commands"},
	}
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  paths,
	})

	for _, want := range []string{
		"cannot access any file or directory named by the configured deny pattern *.env",
		"A denied path appears as an empty unreadable file or directory",
		"configured path /reference is read-only",
		"configured path /output is read-write.",
		"shell can execute files at or under /commands",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}

func TestASessionWithNoDenyPatternIsNeverToldWhatADeniedPathLooksLike(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Write,
	})

	if strings.Contains(got, "A denied path") {
		t.Errorf("system prompt mentions a denied path: %q", got)
	}
}

var pathGrantWording = []string{
	"The user can grant access to paths with /grant, and take them back with /revoke.",
	"Ask the user to grant a needed path rather than working around it or giving up.",
}

func TestTheHarnessDisclosesThatAPathOutOfReachCanBeGranted(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{Read: []string{"/reference"}},
		Conditions:  conditions.Conditions{Interactive: true},
	})

	for _, want := range pathGrantWording {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}

func TestAPathCanBeAskedForEvenWhenNobodyIsListening(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{Read: []string{"/reference"}},
	})

	for _, want := range pathGrantWording {
		if !strings.Contains(got, want) {
			t.Errorf("non-interactive prompt does not contain %q: %q", want, got)
		}
	}
}

func TestClipboardDropsAreDisclosedInTheHarnessContext(t *testing.T) {
	dropsDirectory := "/state/sessions/tame-impala/drops"
	got := harnessContext(Config{
		Workspace:      work.At("/workspace"),
		SessionName:    "tame-impala",
		TmpDir:         "/state/farm/session",
		HomeDir:        "/state/home",
		CurrentCaps:    caps.Read,
		DropsDirectory: dropsDirectory,
	})

	want := "Pasting a clipboard image with ctrl+v saves it under " + dropsDirectory + ", where path tools can read it."
	if !strings.Contains(got, want) {
		t.Errorf("system prompt does not contain %q: %q", want, got)
	}
}

func TestAStoredPromptLearnsAboutClipboardDropsOnce(t *testing.T) {
	dropsDirectory := "/state/sessions/tame-impala/drops"
	got := WithDropsDirectory("stored prompt", dropsDirectory)
	want := "stored prompt\n\n# Clipboard Drops\n\n- " + dropsRule(dropsDirectory)
	if got != want {
		t.Errorf("updated prompt is %q, want %q", got, want)
	}
	if repeated := WithDropsDirectory(got, dropsDirectory); repeated != got {
		t.Errorf("drops rule was repeated: %q", repeated)
	}
}

func TestTheHarnessDisclosesTheSessionName(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "tame-impala",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if !strings.Contains(got, "Your session is named tame-impala") {
		t.Errorf("harness context does not contain the session name: %q", got)
	}
}

func TestTheHarnessGivesTheSessionItsAnimalPersonality(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "tame-impala",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if !strings.Contains(got, "Adopt the personality of the animal in your session name") {
		t.Errorf("harness context does not give the session its animal personality: %q", got)
	}
}

func TestTheHarnessDisclosesLookupAndFetchAccess(t *testing.T) {
	for name, test := range map[string]struct {
		currentCaps      caps.Set
		isNetworkGranted bool
		want             []string
	}{
		"both refused": {
			currentCaps: caps.Read,
			want:        []string{"lookup tool is refused", "fetch tool is refused"},
		},
		"lookup granted": {
			currentCaps: caps.Read | caps.Lookup,
			want:        []string{"lookup tool is granted external network access", "fetch tool is refused"},
		},
		"fetch granted": {
			currentCaps:      caps.Read | caps.Network,
			isNetworkGranted: true,
			want:             []string{"lookup tool is refused", "fetch tool is granted external network access"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext(Config{
				Workspace:      work.At("/workspace"),
				SessionName:    "session-id",
				TmpDir:         "/tmp/x",
				HomeDir:        "/state/home",
				CurrentCaps:    test.currentCaps,
				ExtraPaths:     shell.Paths{},
				NetworkGranted: test.isNetworkGranted,
			})
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("harness context does not contain %q: %q", want, got)
				}
			}
		})
	}
}

func TestTheHarnessDisclosesPrivateLoopbackNetworking(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
		Conditions:  conditions.Conditions{UnixSockets: true, IPv6: true},
	})

	for _, want := range []string{
		"private loopback interface",
		"127.0.0.1 and ::1",
		"Unix sockets work beneath /tmp, but not beneath the workspace",
		"host's loopback interface and external networks are unreachable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessDisclosesWhatThisMachineCannotDo(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"127.0.0.1, and this machine has no IPv6 at all",
		"Unix sockets do not work",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}

	for _, unwanted := range []string{"and ::1", "A Unix socket works"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context still promises %q: %q", unwanted, got)
		}
	}
}

func TestTheHarnessSaysWhenHomeCanBeWrittenTo(t *testing.T) {
	confined := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	want := "HOME is writable only while the workspace is writable, though .cache inside HOME is always writable"
	if !strings.Contains(confined, want) {
		t.Errorf("harness context does not contain %q: %q", want, confined)
	}

	waived := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
		Yolo:        true,
	})

	if want := "- You can write anywhere inside HOME"; !strings.Contains(waived, want) {
		t.Errorf("harness context does not contain %q: %q", want, waived)
	}
}

func TestTheHarnessDisclosesTheHostNetworkACallCanAskFor(t *testing.T) {
	got := harnessContext(Config{
		Workspace:      work.At("/workspace"),
		SessionName:    "session-id",
		TmpDir:         "/tmp/x",
		HomeDir:        "/state/home",
		CurrentCaps:    caps.Read | caps.Shell,
		ExtraPaths:     shell.Paths{},
		NetworkGranted: true,
		Conditions:     conditions.Conditions{Interactive: true},
	})

	for _, want := range []string{
		"By default, the host's loopback interface and external networks are unreachable",
		"The bash tool takes network=loopback or network=host",
		"A call with network=host runs on the host's own network",
		"A host call cannot reach the sandbox's private loopback",
		"The user may be asked to approve each host call",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}

	if unwanted := "Anything else that requires external networking must be asked of the user"; strings.Contains(got, unwanted) {
		t.Errorf("harness context still claims %q: %q", unwanted, got)
	}
}

func TestTheHarnessDoesNotOfferTheHostNetworkWhenItIsNotGranted(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
		ExtraPaths:  shell.Paths{},
		Conditions:  conditions.Conditions{Interactive: true},
	})

	for _, unwanted := range []string{"A call with network=host runs", "By default,"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context offers %q without the network grant: %q", unwanted, got)
		}
	}

	for _, want := range []string{
		"The bash tool takes network=loopback or network=host",
		"The host network is withheld in this session, so a call asking for network=host is refused",
		"The user can grant the host network with ctrl+x n",
		"Ask the user to grant the host network rather than asking the user to run the command",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheScratchMappingIsWrittenInFull(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	scratch := "/home/alice/.local/state/org.crdx/oh/farm/0d3f"
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      scratch,
		HomeDir:     "/home/alice/.local/state/org.crdx/oh/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"It maps to " + scratch + " on the user's machine",
		"/tmp/foo.png → " + scratch + "/foo.png",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessDisclosesTheShellHome(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"path can only access the workspace, private home, /tmp, and read-only system and executable search paths",
		"HOME is /state/home",
		"A tilde (~) for you is not the same as for the user",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessNeverAbbreviatesAPathToATilde(t *testing.T) {
	home := "/home/alice"
	t.Setenv("HOME", home)

	got := harnessContext(Config{
		Workspace:   work.At(filepath.Join(home, "workspace")),
		SessionName: "session-id",
		TmpDir:      filepath.Join(home, ".local", "state", "org.crdx", "oh", "farm", "0d3f"),
		HomeDir:     filepath.Join(home, ".local", "state", "org.crdx", "oh", "home"),
		CurrentCaps: caps.Read | caps.Write | caps.Shell,
		ExtraPaths:  shell.Paths{Read: []string{filepath.Join(home, "reference")}, Write: []string{filepath.Join(home, "output")}, Exec: []string{filepath.Join(home, "commands")}},
	})

	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "~/") {
			t.Errorf("harness context abbreviates a path: %q", line)
		}
	}
}

func TestTheSkillCatalogueIsAppendedToTheContext(t *testing.T) {
	workspace := systemWorkspace(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadTestContext(workspace, []skill.Skill{{
		Name:        "pdf",
		Description: "Work with PDFs.",
		Location:    "/skills/pdf/SKILL.md",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<available_skills>", "<name>pdf</name>", "/skills/pdf/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}

func TestPromptSeparatesTheWorkspaceFromTmp(t *testing.T) {
	system := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if want := "The workspace (/workspace) is " + filesystem(false); !strings.Contains(system, want) {
		t.Errorf("expected the workspace to be reported as %q, got %q", want, system)
	}

	if want := "The workspace is not a git repository"; !strings.Contains(system, want) {
		t.Errorf("expected the absent repository to be reported as %q, got %q", want, system)
	}

	if !strings.Contains(system, "which you can always read and write") {
		t.Errorf("expected the scratch to be writable whatever the workspace is, got %q", system)
	}

	if !strings.Contains(system, "It maps to /state/farm/session on the user's machine") {
		t.Errorf("expected the scratch backing directory to be reported, got %q", system)
	}

	if !strings.Contains(system, "/tmp/foo.png → /state/farm/session/foo.png") {
		t.Errorf("expected an example translated scratch path, got %q", system)
	}

	if strings.Contains(system, "including /tmp") {
		t.Errorf("the workspace mode still claims to include /tmp: %q", system)
	}
}

func TestPromptStatesWhetherTheShellCanRun(t *testing.T) {
	for name, test := range map[string]struct {
		currentCaps caps.Set
		granted     bool
	}{
		"granted": {caps.Read | caps.Shell, true},
		"refused": {caps.Read, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext(Config{
				Workspace:   work.At("/workspace"),
				SessionName: "session-id",
				TmpDir:      "/state/farm/session",
				HomeDir:     "/state/home",
				CurrentCaps: test.currentCaps,
				ExtraPaths:  shell.Paths{},
			})

			if want := "The bash tool is " + shellAccess(test.granted); !strings.Contains(got, want) {
				t.Errorf("expected %q in %q", want, got)
			}

			if unwanted := "The bash tool is " + shellAccess(!test.granted); strings.Contains(got, unwanted) {
				t.Errorf("expected no %q in %q", unwanted, got)
			}
		})
	}
}

func TestAWaivedSandboxIsDisclosedRatherThanImplied(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
		Yolo:        true,
	})

	for _, want := range []string{
		"# No Sandbox",
		"the bash tool runs with no sandbox",
		"There is no network sandbox: everything runs on the host network",
		"/tmp is the machine's own /tmp",
		"Your persistent scratch space is /state/farm/session",
		"The bash tool is granted, and runs unconfined",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}

	for _, unwanted := range []string{
		"private loopback interface",
		"external networks are unreachable",
		"It maps to /state/farm/session on the user's machine",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context still claims %q: %q", unwanted, got)
		}
	}
}

func TestASandboxedSessionIsNeverToldThereIsNoSandbox(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
	})

	for _, unwanted := range []string{"# No Sandbox", "unconfined", "--yolo"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context contains %q: %q", unwanted, got)
		}
	}
}

func TestAToolThatIsNotOfferedIsNeverMentioned(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/tmp/x",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell | caps.Network | caps.Lookup,
		ExtraPaths:   shell.Paths{Exec: []string{"/commands"}},
		OfferedTools: []string{"read", "ls", "grep"},
		JobsGranted:  true,
	})

	for _, unwanted := range []string{
		"# Network",
		"bash",
		"lookup tool",
		"fetch tool",
		"job tool",
		"The shell may execute files",
		"git clone --shared",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context mentions %q with no such tool offered: %q", unwanted, got)
		}
	}
}

func TestASessionWithNoShellIsNeverToldWhatItsShellCouldDo(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/tmp/x",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"read", "ls", "grep"},
		Yolo:         true,
	})

	for _, unwanted := range []string{"# No Sandbox", "bash", "--yolo"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context mentions %q with no shell offered: %q", unwanted, got)
		}
	}
}

func TestAnOfferedToolIsStillMentioned(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/tmp/x",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		ExtraPaths:   shell.Paths{Exec: []string{"/commands"}},
		OfferedTools: []string{"read", "bash", "job"},
		JobsGranted:  true,
	})

	for _, want := range []string{
		"# Network",
		"The bash tool is granted",
		"A service started with the job tool",
		"The shell can execute files at or under /commands.",
		"git clone --shared",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}

	for _, unwanted := range []string{"lookup tool", "fetch tool"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context mentions %q with no such tool offered: %q", unwanted, got)
		}
	}
}

func TestARepositoryWorkspaceReportsItsGitDirectory(t *testing.T) {
	workspace := systemWorkspace(t)
	if err := os.MkdirAll(filepath.Join(workspace.GetDir(), ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := harnessContext(Config{
		Workspace:   workspace,
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
	})

	want := "The .git directory within it (" + filepath.Join(workspace.GetDir(), ".git") + ") is read-only"
	if !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
	if unwanted := "not a git repository"; strings.Contains(got, unwanted) {
		t.Errorf("harness context contains %q for a repository: %q", unwanted, got)
	}
}

func TestAWorkspaceWithNoRepositorySaysSoInsteadOfNamingAGitDirectory(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   systemWorkspace(t),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Git,
	})

	want := "The workspace is not a git repository"
	if !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
	if unwanted := ".git directory within it"; strings.Contains(got, unwanted) {
		t.Errorf("harness context names %q with no repository: %q", unwanted, got)
	}
}

func TestAReadOnlyPathHoldingTheScratchReportsTheScratchAsWritable(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "tame-impala",
		TmpDir:      "/state/farm/tame-impala",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{Read: []string{"/state/farm", "/state/sessions"}},
	})

	for _, want := range []string{
		"The configured path /state/farm is read-only, apart from your scratch space at " +
			"/state/farm/tame-impala, which is writable.",
		"The configured path /state/sessions is read-only.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheShellReportsWhereItMayExecuteFiles(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
	})

	want := "The shell can execute files under the system directories, every directory in PATH, " +
		"the workspace, HOME, and /tmp."
	if !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
}

func TestTheShellAndPathToolsReportSystemReadAccess(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
	})

	for _, want := range []string{
		"Tools that accept a path can only access the workspace, private home, /tmp, and read-only system and executable search paths.",
		"The shell shares those read grants and additionally sees private process, terminal, resolver, and language-package cache files needed to run commands.",
		"The shell has the same write access as path tools; runtime devices are the only additional writable exceptions.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestAnUnconfinedShellDoesNotReportWhereItMayRead(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
		Yolo:         true,
	})

	if unwanted := "private process, terminal, resolver, and language-package cache files"; strings.Contains(got, unwanted) {
		t.Errorf("harness context mentions %q with no sandbox: %q", unwanted, got)
	}
	if want := "The shell is unconfined in --yolo mode; path tools remain limited to the paths above."; !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
}

func TestAnUnconfinedShellReportsNoExecutableDirectories(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		ExtraPaths:   shell.Paths{Exec: []string{"/commands"}},
		OfferedTools: []string{"bash"},
		Yolo:         true,
	})

	if unwanted := "may execute files"; strings.Contains(got, unwanted) {
		t.Errorf("harness context mentions %q with no sandbox: %q", unwanted, got)
	}
}

func TestTheHarnessDisclosesThatABashCallTakesItsProcessesWithIt(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash", "job"},
		JobsGranted:  true,
	})

	want := "A process a bash call leaves running is killed when that call ends, so start anything " +
		"that must outlive the call with the job tool"
	if !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
}

func TestTheReadOnlyWorkspaceWorkflowHasItsOwnSection(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
	})

	for _, rule := range []string{
		"no workflow below covers it",
		"# Read-only Workspaces",
		"use this workflow instead of asking for write access",
		"git clone --shared <workspace> <destination>",
		"git -C <workspace> diff --binary HEAD",
		"Do the work and run its checks in the scratch copy",
		"hand off only unapplied patches",
		"git -C <workspace> apply --check <patch>",
	} {
		if !strings.Contains(got, rule) {
			t.Errorf("read-only workspace workflow does not contain %q: %q", rule, got)
		}
	}

	state := strings.Index(got, "# State")
	workflow := strings.Index(got, "# Read-only Workspaces")
	if state == -1 || workflow <= state {
		t.Errorf("read-only workspace workflow is not its own section after state: %q", got)
	}
}

func TestWaitingForTheUserFollowsJobAvailability(t *testing.T) {
	for name, testCase := range map[string]struct {
		jobsGranted bool
		expected    bool
	}{
		"available":   {jobsGranted: true, expected: true},
		"unavailable": {jobsGranted: false, expected: false},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext(Config{
				Workspace:    work.At("/workspace"),
				SessionName:  "session-id",
				TmpDir:       "/state/farm/session",
				HomeDir:      "/state/home",
				CurrentCaps:  caps.Read | caps.Shell,
				OfferedTools: []string{"bash", "job"},
				JobsGranted:  testCase.jobsGranted,
			})

			for _, rule := range []string{
				"# Waiting for the User",
				"If waiting on the user, start a job",
				"command whose success proves completion",
				"recheck after each relevant event until the command succeeds",
				"if unavailable, poll with a modest delay",
				"Once the job completes, continue",
			} {
				present := strings.Contains(got, rule)
				if present != testCase.expected {
					t.Errorf("waiting rule presence is %t, want %t: %q", present, testCase.expected, got)
				}
			}
			if unwanted := "Before handing a patch off, start a background job"; strings.Contains(got, unwanted) {
				t.Errorf("patch-specific waiting rule remains in the harness context: %q", got)
			}
		})
	}
}

func TestTheHarnessOffersNoJobToolForAProcessThatMustOutliveACall(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
	})

	want := "A process a bash call leaves running is killed when that call ends"
	if !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
	if unwanted := "with the job tool"; strings.Contains(got, unwanted) {
		t.Errorf("harness context offers %q with no such tool: %q", unwanted, got)
	}
}

func TestAnUnconfinedShellKeepsItsBackgroundProcessesUnmentioned(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash", "job"},
		JobsGranted:  true,
		Yolo:         true,
	})

	if unwanted := "leaves running is killed"; strings.Contains(got, unwanted) {
		t.Errorf("harness context mentions %q with no sandbox: %q", unwanted, got)
	}
}

func TestEveryConfiguredPathKindHasItsFileToolAccessDocumented(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	homePath := filepath.Join(userHome, ".config", "git", "ignore")
	outsideHomePath := filepath.Join(t.TempDir(), "git", "ignore")
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
		ExtraPaths: shell.Paths{
			Exec: []string{"/commands"},
			Path: []string{"/toolbox/bin"},
			Home: []string{homePath, outsideHomePath},
		},
	})

	for _, want := range []string{
		"The configured executable path /commands is read-only to path tools.",
		"The configured PATH directory /toolbox/bin is read-only to path tools.",
		"The configured home path " + homePath + " is read-only and exposed at HOME/.config/git/ignore.",
		"The configured home path " + outsideHomePath + " is read-only to path tools but cannot be exposed in private HOME because it is outside the user's home.",
		"The shell can execute files at or under /commands.",
		"The shell can execute files at or under /toolbox/bin, which is in PATH.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestAConfinedJobDocumentsItsNetworkDifferenceFromBash(t *testing.T) {
	got := harnessContext(Config{
		Workspace:    work.At("/workspace"),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell | caps.Network,
		OfferedTools: []string{"bash", "job"},
		JobsGranted:  true,
	})

	if want := "A job command has only private loopback networking and cannot request the host network"; !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
	if want := "The bash tool takes network=loopback or network=host"; !strings.Contains(got, want) {
		t.Errorf("harness context does not contain %q: %q", want, got)
	}
}

func TestTitlesAndNotificationsFollowTheirOwnTools(t *testing.T) {
	for name, test := range map[string]struct {
		offeredTools []string
		wanted       []string
		unwanted     []string
	}{
		"both offered": {
			offeredTools: []string{"title", "notify"},
			wanted:       []string{"# Titles", "title tool", "# Notifications", "notify tool"},
		},
		"title alone": {
			offeredTools: []string{"title"},
			wanted:       []string{"# Titles", "title tool"},
			unwanted:     []string{"# Notifications", "notify tool"},
		},
		"notify alone": {
			offeredTools: []string{"notify"},
			wanted:       []string{"# Notifications", "notify tool"},
			unwanted:     []string{"# Titles", "title tool"},
		},
		"neither offered": {
			offeredTools: []string{"sysinfo"},
			unwanted:     []string{"# Titles", "title tool", "# Notifications", "notify tool"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext(Config{
				Workspace:    work.At("/workspace"),
				SessionName:  "session-id",
				CurrentCaps:  caps.Read,
				OfferedTools: test.offeredTools,
			})

			for _, want := range test.wanted {
				if !strings.Contains(got, want) {
					t.Errorf("harness context omits %q: %q", want, got)
				}
			}
			for _, unwanted := range test.unwanted {
				if strings.Contains(got, unwanted) {
					t.Errorf("harness context names %q with no such tool offered: %q", unwanted, got)
				}
			}
		})
	}
}
