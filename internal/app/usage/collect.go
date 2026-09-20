package usage

import (
	"context"
	"strconv"
	"sync"
	"time"

	"crdx.org/io/pkg/agent"
)

const DefaultRefresh = 5 * time.Minute

type Source struct {
	Provider  string
	Label     string
	Reporter  agent.UsageReporter
	CachePath string

	HasIdleSessionWindow bool
	IsSelfRefreshing     bool
}

type Options struct {
	JSON bool
}

type result struct {
	windows    []agent.UsageWindow
	measuredAt time.Time
	failure    string
}

type cacheReader struct{}

func (cacheReader) IsAvailable() bool {
	return false
}

func (cacheReader) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	return nil, nil
}

func Collect(ctx context.Context, sources []Source, now func() time.Time) Report {
	refreshAfter := DefaultRefresh

	results := make([]result, len(sources))

	var group sync.WaitGroup
	for i, source := range sources {
		group.Go(func() {
			results[i] = gather(ctx, source, refreshAfter, now)
		})
	}
	group.Wait()

	report := Report{SchemaVersion: SchemaVersion, Providers: []Snapshot{}}

	for i, source := range sources {
		if !isWorthShowing(source, results[i]) {
			continue
		}

		report.Providers = append(
			report.Providers, buildSnapshot(source, results[i], refreshAfter, now()),
		)
	}

	return report
}

func isWorthShowing(source Source, outcome result) bool {
	return len(outcome.windows) > 0 || outcome.failure != "" || source.Reporter != nil
}

func gather(
	ctx context.Context, source Source, refreshAfter time.Duration, now func() time.Time,
) result {
	liveReporter := source.Reporter
	if liveReporter == nil {
		liveReporter = cacheReader{}
	}

	reporter := Shared(liveReporter, source.CachePath, refreshAfter, now)
	snapshotter, isShared := reporter.(Snapshotter)

	windows, err := reporter.UsageWindows(ctx)
	if err != nil {
		outcome := result{failure: failureReason(err)}

		if isShared {
			outcome.windows, outcome.measuredAt = snapshotter.GetSnapshot()
		}

		return outcome
	}

	measuredAt := now()
	if isShared {
		_, measuredAt = snapshotter.GetSnapshot()
	}

	return result{windows: windows, measuredAt: measuredAt}
}

func failureReason(err error) string {
	if status, isRefusal := FailureStatus(err); isRefusal {
		return "refused with " + strconv.Itoa(status)
	}

	return err.Error()
}
