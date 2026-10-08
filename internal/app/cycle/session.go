package cycle

import "crdx.org/oh/internal/app/model"

const (
	sourceSessionOption = "--from"
	modelOption         = "-m"
	capsOption          = "-c"
	toolOption          = "-t"
	yoloOption          = "--yolo"
)

type SessionOptions struct {
	ModelGlob string
	CapFlags  string
	Tools     []string
	IsYolo    bool
}

func NewSessionTransition(options SessionOptions, choices []model.Choice, defaults model.Defaults) (Transition, error) {
	return selectedOptionsTransition(options, choices, defaults)
}

func ForkedSessionTransition(
	options SessionOptions,
	choices []model.Choice,
	defaults model.Defaults,
	sourceSessionName string,
) (Transition, error) {
	transition, err := selectedOptionsTransition(options, choices, defaults)
	if err != nil {
		return Transition{}, err
	}

	transition.Arguments = append(transition.Arguments, sourceSessionOption, sourceSessionName)
	return transition, nil
}

func selectedOptionsTransition(options SessionOptions, choices []model.Choice, defaults model.Defaults) (Transition, error) {
	transition := Transition{Kind: NewSession}
	if options.ModelGlob != "" {
		selection, err := model.ResolveQuery(options.ModelGlob, choices, defaults)
		if err != nil {
			return Transition{}, err
		}
		transition.Arguments = append(transition.Arguments, modelOption, selection.String())
	}
	if options.CapFlags != "" {
		transition.Arguments = append(transition.Arguments, capsOption, options.CapFlags)
	}
	for _, name := range options.Tools {
		transition.Arguments = append(transition.Arguments, toolOption, name)
	}
	if options.IsYolo {
		transition.Arguments = append(transition.Arguments, yoloOption)
	}

	return transition, nil
}
