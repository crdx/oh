package prompt

const (
	guidanceHeading    = "Model Guidance"
	guidancePrecedence = "- Every other instruction in this prompt, and every instruction from the user, takes precedence over this section"
)

func GuidanceSection(guidance string) string {
	if guidance == "" {
		return ""
	}

	return section(guidanceHeading, []string{guidancePrecedence}) + "\n\n" + guidance
}
