package cycle

import "crdx.org/oh/internal/app/model"

const sourceSessionOption = "--from"

func NewSessionTransition(modelGlob string, choices []model.Choice, defaults model.Defaults) (Transition, error) {
	return selectedModelTransition(modelGlob, choices, defaults)
}

func ForkedSessionTransition(
	modelGlob string,
	choices []model.Choice,
	defaults model.Defaults,
	sourceSessionName string,
) (Transition, error) {
	transition, err := selectedModelTransition(modelGlob, choices, defaults)
	if err != nil {
		return Transition{}, err
	}

	transition.Arguments = append(transition.Arguments, sourceSessionOption, sourceSessionName)
	return transition, nil
}

func selectedModelTransition(modelGlob string, choices []model.Choice, defaults model.Defaults) (Transition, error) {
	transition := Transition{Kind: NewSession}
	if modelGlob == "" {
		return transition, nil
	}

	selection, err := model.ResolveQuery(modelGlob, choices, defaults)
	if err != nil {
		return Transition{}, err
	}

	transition.Arguments = []string{"-m", selection.String()}
	return transition, nil
}
