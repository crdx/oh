package agent_test

import (
	"context"
	"slices"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type notedProvider struct {
	messages []string
	format   agent.NoteFormat
}

func (*notedProvider) Configure(string, []tool.Definition) {}

func (self *notedProvider) AddUserMessage(text string) {
	self.messages = append(self.messages, text)
}

func (*notedProvider) AddToolResults([]agent.ToolCallResult) {}

func (*notedProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type formattedProvider struct {
	notedProvider
}

func (self *formattedProvider) NoteFormat() agent.NoteFormat {
	return self.format
}

type taggedNotes struct{}

func (taggedNotes) Wrap(note agent.Note) string {
	return agent.WrapNote(`context source="`+string(note.Kind)+`"`, note.Text)
}

func (taggedNotes) Rule() string {
	return ""
}

func TestEachNoteIsWrappedInASystemReminderByDefault(t *testing.T) {
	provider := &notedProvider{}
	assistant := agent.New("", provider, nil)

	assistant.AddNotes([]agent.Note{
		{Kind: agent.EnvironmentNote, Text: "Granted temporary read-only access to /work/fleet."},
		{Kind: agent.TitleNote},
		{Kind: agent.TitleNote, Text: "The session is still untitled. Use the title tool to set a title."},
	})
	assistant.AddUserMessage("fyi")

	want := []string{
		"<system-reminder>\nGranted temporary read-only access to /work/fleet.\n</system-reminder>\n\n" +
			"<system-reminder>\nThe session is still untitled. Use the title tool to set a title.\n</system-reminder>",
		"fyi",
	}
	if !slices.Equal(provider.messages, want) {
		t.Errorf("got %q, want %q", provider.messages, want)
	}
}

func TestAProviderWrapsNotesItsOwnWay(t *testing.T) {
	provider := &formattedProvider{notedProvider{format: taggedNotes{}}}
	assistant := agent.New("", provider, nil)

	assistant.AddNotes([]agent.Note{{Kind: agent.JobNote, Text: "Job `web` ended."}})

	want := []string{"<context source=\"job\">\nJob `web` ended.\n</context>"}
	if !slices.Equal(provider.messages, want) {
		t.Errorf("got %q, want %q", provider.messages, want)
	}
}

func TestNoNotesAddNoMessage(t *testing.T) {
	provider := &notedProvider{}
	assistant := agent.New("", provider, nil)

	assistant.AddNotes([]agent.Note{{Kind: agent.InterruptionNote}})
	assistant.AddNotes(nil)

	if len(provider.messages) != 0 {
		t.Errorf("got %q, want nothing sent", provider.messages)
	}
}
