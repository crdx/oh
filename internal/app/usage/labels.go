package usage

import (
	"strings"
	"time"

	"crdx.org/io/internal/app/model"
	"crdx.org/io/pkg/agent"
)

const (
	sessionLength = 5 * time.Hour
	weeklyLength  = 7 * dayLength
	monthlyLength = 30 * dayLength
)

var windowNames = map[time.Duration]string{
	sessionLength: "Session",
	weeklyLength:  "Week",
	monthlyLength: "Month",
}

var shortWindowNames = map[time.Duration]string{
	sessionLength: "5h",
	weeklyLength:  "wk",
	monthlyLength: "mo",
}

func WindowLabel(window agent.UsageWindow) string {
	if window.Scope != "" {
		return ScopeLabel(window.Scope) + " " + windowName(window.Duration)
	}

	return windowName(window.Duration)
}

func ShortWindowLabel(duration time.Duration) string {
	if name, isNamed := shortWindowNames[duration]; isNamed {
		return name
	}

	return DurationLabel(duration)
}

func windowName(duration time.Duration) string {
	if name, isNamed := windowNames[duration]; isNamed {
		return name
	}

	return DurationLabel(duration)
}

func ScopeLabel(scope string) string {
	name := strings.ToLower(strings.TrimSpace(scope))

	if _, suffix, found := strings.Cut(name, "-codex-"); found {
		name = suffix
	}

	name = strings.ReplaceAll(strings.TrimPrefix(name, "claude-"), "_", "-")

	if name == "" {
		return scope
	}

	return strings.Join(model.DisplayName(name), " ")
}
