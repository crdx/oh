package agent

import "strings"

type NoteKind string

const (
	InterruptionNote NoteKind = "interruption"
	HostCommandNote  NoteKind = "host_command"
	EnvironmentNote  NoteKind = "environment"
	JobNote          NoteKind = "job"
	TitleNote        NoteKind = "title"
	PokeNote         NoteKind = "poke"
)

type Note struct {
	Kind NoteKind
	Text string
}

type NoteFormat interface {
	Wrap(note Note) string
	Rule() string
}

type NoteFormatter interface {
	NoteFormat() NoteFormat
}

const noteSeparator = "\n\n"

var SystemReminders NoteFormat = systemReminders{}

type systemReminders struct{}

func (systemReminders) Wrap(note Note) string {
	return WrapNote("system-reminder", note.Text)
}

func (systemReminders) Rule() string {
	return "User messages may include <system-reminder> tags. They are added automatically by the harness, and are not written by the user."
}

func WrapNote(tag string, text string) string {
	return "<" + tag + ">\n" + text + "\n</" + closingTag(tag) + ">"
}

func closingTag(tag string) string {
	name, _, _ := strings.Cut(tag, " ")
	return name
}

func NoteFormatOf(provider Provider) NoteFormat {
	if formatter, isFormatted := provider.(NoteFormatter); isFormatted {
		if format := formatter.NoteFormat(); format != nil {
			return format
		}
	}

	return SystemReminders
}

func FormatNotes(format NoteFormat, notes []Note) string {
	texts := make([]string, 0, len(notes))
	for _, note := range notes {
		if note.Text == "" {
			continue
		}
		texts = append(texts, format.Wrap(note))
	}

	return strings.Join(texts, noteSeparator)
}

func (self *Agent) AddNotes(notes []Note) {
	if text := FormatNotes(NoteFormatOf(self.provider), notes); text != "" {
		self.provider.AddUserMessage(text)
	}
}
