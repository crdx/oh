package util

import (
	"fmt"
	"time"
)

func CompactDuration(elapsedTime time.Duration) string {
	if elapsedTime > -time.Second && elapsedTime < time.Second {
		tenths := elapsedTime.Round(100 * time.Millisecond).Seconds()
		if tenths == 0 {
			return "0s"
		}
		return fmt.Sprintf("%.1fs", tenths)
	}
	elapsedTime = elapsedTime.Truncate(time.Second)

	switch {
	case elapsedTime%time.Second == 0 && elapsedTime < time.Minute:
		return fmt.Sprintf("%ds", int(elapsedTime.Seconds()))
	case elapsedTime%time.Minute == 0 && elapsedTime < time.Hour:
		return fmt.Sprintf("%dm", int(elapsedTime.Minutes()))
	case elapsedTime%time.Hour == 0 && elapsedTime < 100*time.Hour:
		return fmt.Sprintf("%dh", int(elapsedTime.Hours()))
	}
	return FormatDuration(elapsedTime)
}

func CoarseDuration(elapsedTime time.Duration) string {
	switch {
	case elapsedTime < time.Minute:
		return "<1m"
	case elapsedTime < time.Hour:
		return fmt.Sprintf("%dm", int(elapsedTime.Minutes()))
	case elapsedTime < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsedTime.Hours()))
	}

	return fmt.Sprintf("%dd", int(elapsedTime.Hours()/24))
}

func Ago(when time.Time) string {
	elapsedTime := time.Since(when)
	if elapsedTime < time.Minute {
		return "just now"
	}

	return CoarseDuration(elapsedTime) + " ago"
}

func FormatDuration(elapsedTime time.Duration) string {
	switch {
	case elapsedTime < time.Second:
		return fmt.Sprintf("0.%ds", int(elapsedTime.Milliseconds()%1000)/100)
	case elapsedTime < time.Minute:
		return fmt.Sprintf("%ds", int(elapsedTime.Seconds()))
	case elapsedTime < time.Hour:
		return fmt.Sprintf("%dm %ds", int(elapsedTime.Minutes()), int(elapsedTime.Seconds())%60)
	case elapsedTime < 100*time.Hour:
		return fmt.Sprintf("%dh %dm", int(elapsedTime.Hours()), int(elapsedTime.Minutes())%60)
	}

	days := int(elapsedTime.Hours()) / 24
	switch {
	case days < 100:
		return fmt.Sprintf("%dd %dh", days, int(elapsedTime.Hours())%24)
	case days <= 9999:
		return fmt.Sprintf("%dd", days)
	default:
		return "9999d+"
	}
}
