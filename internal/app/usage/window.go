package usage

import (
	"fmt"
	"time"

	"crdx.org/oh/pkg/agent"
)

const (
	dayLength      = 24 * time.Hour
	percentCeiling = 100

	overPacePercent = 10
	overPaceRatio   = 1.5
	nearLimit       = 90
)

type Pace int

const (
	PaceEven Pace = iota
	PaceAhead
	PaceCritical
)

func ExpectedPercent(window agent.UsageWindow, at time.Time) int {
	start := window.ResetsAt.Add(-window.Duration)

	elapsedTime := at.Sub(start)
	if elapsedTime < 0 {
		return 0
	}

	percent := int(elapsedTime * percentCeiling / window.Duration)

	return min(percentCeiling, percent)
}

func ClassifyPace(actual int, expectedPercentage int) Pace {
	if actual < overPacePercent || actual <= expectedPercentage {
		return PaceEven
	}

	if actual >= nearLimit || float64(actual) >= float64(expectedPercentage)*overPaceRatio {
		return PaceCritical
	}

	return PaceAhead
}

func DurationLabel(duration time.Duration) string {
	switch {
	case duration >= dayLength && duration%dayLength == 0:
		return fmt.Sprintf("%dd", duration/dayLength)
	case duration >= time.Hour && duration%time.Hour == 0:
		return fmt.Sprintf("%dh", duration/time.Hour)
	default:
		return fmt.Sprintf("%dm", duration/time.Minute)
	}
}
