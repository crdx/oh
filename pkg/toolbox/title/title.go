package title

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"unicode/utf8"

	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const (
	Name           = "title"
	maxTitleLength = 50
)

type Args struct {
	Title string `json:"title"`
}

func New() tool.Tool {
	givenTitles := &titles{}

	return tool.Implement(
		tool.Definition{
			Name: Name,
			Description: "name the task this session is doing, in a few words, " +
				"as soon as the task is clear; ensure to retitle as the session evolves",
			Schema: tool.Schema{
				tool.String("title", `a 3-word slug description of the task, like "fix-picker-clipping"`),
			},
		},
		Describe,
	).
		State(agent.TitleStateKey, givenTitles.restore).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Validate(validate).
		Run(givenTitles.exec)
}

func Describe(args Args) (string, string) {
	return strutil.Flatten(args.Title), ""
}

func validate(args Args) error {
	title := strutil.Flatten(args.Title)
	if title == "" {
		return errors.New("title is required")
	}
	if utf8.RuneCountInString(title) > maxTitleLength {
		return fmt.Errorf("title exceeds %d characters", maxTitleLength)
	}

	return nil
}

type titles struct {
	mutex   sync.Mutex
	current string
}

func (self *titles) exec(_ context.Context, args Args) (tool.ToolCallResult, error) {
	title := strutil.Flatten(args.Title)

	self.mutex.Lock()
	isUnchanged := title == self.current
	self.mutex.Unlock()

	if isUnchanged {
		return tool.ToolCallResult{Output: "title unchanged: " + strconv.Quote(title)}, nil
	}

	state, err := json.Marshal(agent.TitleState{Title: title})
	if err != nil {
		return tool.ToolCallResult{}, err
	}

	return tool.ToolCallResult{
		Output: "session titled " + strconv.Quote(title),
		State:  state,
	}, nil
}

func (self *titles) restore(payload json.RawMessage) error {
	var state agent.TitleState
	if err := json.Unmarshal(payload, &state); err != nil {
		return err
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.current = state.Title

	return nil
}
