package harness

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/question"
)

func allowApprovalOnce(chat *App) {
	chat.question.cursor = onceApprovalIndex
	chat.answerQuestion(key.Key{Code: key.Enter})
}

func TestGoldenApprovalsDrawCallerDefinedLifetimes(t *testing.T) {
	passes := map[string]func() string{}
	ansiPasses := map[string]func() string{}
	for _, columns := range []int{30, replayColumns} {
		for index, option := range fetchApproval.question("").Options {
			name := fmt.Sprintf("%d columns with choice %q", columns, option.Label)
			ansiPasses[name] = func() string {
				var stream string
				synctest.Test(t, func(t *testing.T) {
					var screenOutput strings.Builder
					chat := slashCommandFixture(t, caps.Read)
					chat.inputLine = edit.NewInput(nil)
					chat.screen = output.NewTerminalOfSize(&screenOutput, columns, replayLines)
					broker := question.NewBroker()
					t.Cleanup(broker.Open())
					chat.question.broker = broker
					result := make(chan error, 1)
					go func() {
						result <- fetchApproval.ask(t.Context(), broker, permission.Setting{Rule: permission.Ask}, "https://example.com")
					}()
					synctest.Wait()
					chat.onQuestionChange()
					chat.answerQuestion(key.Key{Code: key.Left})
					for range index {
						chat.answerQuestion(key.Key{Code: key.Right})
					}
					chat.show(chat.inputLine)
					stream = screenOutput.String()
					chat.answerQuestion(key.Key{Code: key.Enter})
					err := <-result
					if (err != nil) != (index == denyApprovalIndex) {
						t.Fatalf("choice %d returned %v", index, err)
					}
				})
				return stream
			}
			passes[name] = func() string {
				return strings.Join(playScreenOfSize(t, ansiPasses[name](), columns, replayLines).text(), "\n")
			}
		}
	}
	compareWithGolden(t, "timed-approvals", ".screen", passes)
	compareWithGolden(t, "timed-approvals", ".ansi", ansiPasses)
}

func TestAnInteractiveTimedApprovalCoversOnlyTheSameCustomTool(t *testing.T) {
	rig, endpoint := newEndpointRig(t, sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"London"}`}}},
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"York"}`}}},
			{Calls: []sim.Call{{Name: "forecast", Arguments: `{"city":"Bath"}`}}},
			{Say: "Timed approvals finished."},
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"Leeds"}`}}},
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"Derby"}`}}},
			{Say: "Session approvals finished."},
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"Exeter"}`}}},
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"Truro"}`}}},
			{Say: "Revocation finished."},
		},
	}))
	script := declaredToolScript(t)
	var configuration strings.Builder
	for _, name := range []string{"weather", "forecast"} {
		fmt.Fprintf(&configuration, `[tool.%s]
description = "report the weather"
command = ["%s"]
permission = "ask"
group = "a"
parameters = [{ name = "city", kind = "string", description = "the city" }]
`, name, script)
	}
	writeRigConfig(t, rig, configuration.String())
	session := rig.start("--yolo", "-m", "opencode-go/fake", "-c", "a", "check the weather twice, then the forecast")
	session.waitFor("Run the weather tool?")
	session.typeAndSettle("\t")
	session.requireShown("Allow for 5 mins")
	session.typeText(pressEnter)
	session.waitFor("Run the forecast tool?")
	session.typeAndSettle(pressLeft)
	session.typeText(pressEnter)
	session.waitFor("Timed approvals finished.")
	session.typeText("/grants" + pressEnter)
	session.waitFor("Approved:")
	session.requireShown("tool:weather")
	session.typeAndSettle(pressEscape)
	session.typeAndSettle("/revoke ")
	session.requireShown("tool:weather")
	session.typeText("tool:weather" + pressEnter)
	session.waitFor("Revoked the approval for tool:weather.")
	session.typeAndSettle(pressEscape)
	questionCount := session.Count("Run the weather tool?")
	session.typeText("check again" + pressEnter)
	session.waitForCount("Run the weather tool?", questionCount+1)
	session.typeAndSettle("\t\t")
	session.requireShown("Allow for session")
	session.typeText(pressEnter)
	session.waitFor("Session approvals finished.")
	session.typeText("/grants" + pressEnter)
	session.waitFor("until you quit")
	session.typeAndSettle(pressEscape)
	forgetCount := session.Count("Revoked the approval for tool:weather.")
	session.typeText("/revoke tool:weather" + pressEnter)
	session.waitForCount("Revoked the approval for tool:weather.", forgetCount+1)
	session.typeAndSettle(pressEscape)
	questionCount = session.Count("Run the weather tool?")
	session.typeText("check after revocation" + pressEnter)
	session.waitForCount("Run the weather tool?", questionCount+1)
	session.requireShown("Allow once")
	questionCount = session.Count("Run the weather tool?")
	session.typeText(pressEnter)
	session.waitForCount("Run the weather tool?", questionCount+1)
	session.requireShown("Truro")
	session.typeAndSettle(pressLeft)
	session.typeText(pressEnter)
	session.waitFor("Revocation finished.")
	session.quit()
	var results []string
	for _, request := range endpoint.Requests() {
		for _, entry := range request.Input {
			if entry.Type == sim.CallOutput {
				results = append(results, entry.Output)
			}
		}
	}
	joined := strings.Join(results, "\n")
	for _, expected := range []string{"forecast for --city London", "forecast for --city York", "forecast refused", "forecast for --city Leeds", "forecast for --city Derby", "forecast for --city Exeter", "weather refused"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing %q in results: %s", expected, joined)
		}
	}
}

func TestQuestionUsesOnlyTheOptionsAndLifetimesItsCallerSupplies(t *testing.T) {
	for _, option := range []question.Option{
		{Key: 'm', Label: "Markdown", RememberFor: 30 * time.Second},
		{Key: 'm', Label: "Markdown", RememberForSession: true},
	} {
		synctest.Test(t, func(t *testing.T) {
			broker := question.NewBroker()
			t.Cleanup(broker.Open())
			options := []question.Option{{Key: 'p', Label: "Plain"}, option}
			answers := make(chan int, 1)
			go func() {
				index, _ := broker.Ask(t.Context(), question.Question{Key: "format", Label: "Choose a format", Options: options})
				answers <- index
			}()
			synctest.Wait()
			chat := &App{question: questionState{broker: broker}}
			chat.onQuestionChange()
			if !slices.Equal(chat.question.request.Question.Options, options) {
				t.Fatal("the interface changed the caller's options")
			}
			at := time.Now()
			chat.answerQuestion(key.Key{Code: key.Rune, Value: 'm'})
			if index := <-answers; index != 1 {
				t.Fatalf("got original option %d, want Markdown", index)
			}
			memory := broker.RememberedAnswers()
			if len(memory) != 1 || memory[0].Option != option {
				t.Fatalf("remembered the wrong option: %+v", memory)
			}
			if option.RememberForSession && !memory[0].ExpiresAt.IsZero() || option.RememberFor > 0 && !memory[0].ExpiresAt.Equal(at.Add(option.RememberFor)) {
				t.Fatalf("the interface changed the caller's lifetime: %+v", memory)
			}
		})
	}
}

func TestApprovalChoicesHaveNoEditorOrLetterShortcuts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		result := make(chan error, 1)
		go func() {
			result <- fetchApproval.ask(t.Context(), broker, permission.Setting{Rule: permission.Ask}, "https://example.com")
		}()
		synctest.Wait()
		chat := &App{question: questionState{broker: broker}}
		chat.onQuestionChange()
		want := []question.Option{
			{Label: "Deny"},
			{Label: "Allow once"},
			{Label: "Allow for 5 mins", RememberFor: 5 * time.Minute},
			{Label: "Allow for session", RememberForSession: true},
		}
		if chat.question.cursor != onceApprovalIndex || !slices.Equal(broker.Current().Question.Options, want) {
			t.Fatalf("unexpected default or options: %+v", broker.Current().Question)
		}
		chat.answerQuestion(key.Key{Code: key.Right})
		for _, letter := range "tsyn15" {
			chat.answerQuestion(key.Key{Code: key.Rune, Value: letter})
			if chat.question.cursor != timedApprovalIndex || broker.Current() == nil {
				t.Fatalf("%q changed or answered the question", letter)
			}
		}
		at := time.Now()
		chat.answerQuestion(key.Key{Code: key.Enter})
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		memory := broker.RememberedAnswers()
		if len(memory) != 1 || !memory[0].ExpiresAt.Equal(at.Add(5*time.Minute)) {
			t.Fatalf("typing changed the declared five-minute lifetime: %+v", memory)
		}
	})
}

func TestApprovalKindsUseConsistentRevocationKeys(t *testing.T) {
	for _, test := range []struct {
		approval approval
		key      string
	}{
		{approval: fetchApproval, key: "tool:fetch"},
		{approval: lookupApproval, key: "tool:lookup"},
		{approval: customToolApproval("commit"), key: "tool:commit"},
		{approval: hostNetworkApproval, key: "network:host"},
	} {
		t.Run(test.key, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				broker := question.NewBroker()
				t.Cleanup(broker.Open())
				result := make(chan error, 1)
				go func() {
					result <- test.approval.ask(t.Context(), broker, permission.Setting{Rule: permission.Ask}, "subject")
				}()
				synctest.Wait()
				request := broker.Current()
				if request.Question.Key != test.key {
					t.Fatalf("got key %q, want %q", request.Question.Key, test.key)
				}
				request.Choose(sessionApprovalIndex)
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				if memory := broker.RememberedAnswers(); len(memory) != 1 || memory[0].Key != test.key {
					t.Fatalf("remembered a different key: %+v", memory)
				}
			})
		})
	}
}
