package agent

type Guide interface {
	Guidance() string
}

func GuidanceOf(provider Provider) string {
	if guide, isGuide := provider.(Guide); isGuide {
		return guide.Guidance()
	}

	return ""
}
