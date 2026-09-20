package schedule

import "time"

func Soonest(due ...time.Time) time.Time {
	var soonest time.Time

	for _, at := range due {
		if at.IsZero() {
			continue
		}

		if soonest.IsZero() || at.Before(soonest) {
			soonest = at
		}
	}

	return soonest
}

func NextTick(at time.Time, granularity time.Duration) time.Time {
	return at.Truncate(granularity).Add(granularity)
}
