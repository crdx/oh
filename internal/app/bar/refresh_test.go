package bar

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/jobs"
)

const (
	refreshSpan         = 10 * time.Second
	soonestRefresh      = time.Millisecond
	idleRefreshEvery    = time.Second
	runningRefreshEvery = 100 * time.Millisecond
)

type refreshWorld struct {
	name      string
	workspace func(t *testing.T) string
	isRunning bool
	getJobs   func() []jobs.Snapshot
}

func TestNoSegmentAsksToBeDrawnMoreOftenThanItCanChange(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	noJobs := func() []jobs.Snapshot { return nil }
	liveJob := func() []jobs.Snapshot {
		return []jobs.Snapshot{{Name: "serve", State: jobs.StateRunning, StartedAt: time.Now()}}
	}
	endedJob := func() []jobs.Snapshot {
		return []jobs.Snapshot{{Name: "build", State: jobs.StateComplete, StartedAt: time.Now(), EndedAt: time.Now()}}
	}

	var worlds []refreshWorld
	for _, workspace := range []struct {
		name string
		make func(t *testing.T) string
	}{
		{name: "outside a repository", make: plainWorkspace},
		{name: "in a repository", make: repositoryWorkspace},
		{name: "in a broken repository", make: brokenRepositoryWorkspace},
	} {
		for _, isRunning := range []bool{false, true} {
			for jobName, getJobs := range map[string]func() []jobs.Snapshot{"no jobs": noJobs, "a live job": liveJob, "an ended job": endedJob} {
				turnState := "idle"
				if isRunning {
					turnState = "running"
				}
				worlds = append(worlds, refreshWorld{
					name:      workspace.name + ", " + turnState + ", " + jobName,
					workspace: workspace.make,
					isRunning: isRunning,
					getJobs:   getJobs,
				})
			}
		}
	}

	settings, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}

	for _, world := range worlds {
		t.Run(world.name, func(t *testing.T) {
			workspaceDir := world.workspace(t)
			labels := slices.Sorted(maps.Keys(drawnSegments(t, settings, refreshRegistry(workspaceDir, world))))

			for _, label := range labels {
				t.Run(label, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						subject := drawnSegments(t, settings, refreshRegistry(workspaceDir, world))[label]
						draws, isRefresher := drawsOverSpan(subject, world.isRunning)
						if !isRefresher {
							return
						}

						if limit := refreshLimit(world.isRunning); draws > limit {
							t.Errorf("drawn %d times in %s, want at most %d", draws, refreshSpan, limit)
						}
					})
				})
			}
		})
	}
}

func refreshLimit(isRunning bool) int {
	every := idleRefreshEvery
	if isRunning {
		every = runningRefreshEvery
	}

	return int(refreshSpan/every) + 1
}

func drawnSegments(t *testing.T, settings config.Config, registry segment.Registry) map[string]segment.Segment {
	t.Helper()

	layout, err := settings.BuildLayout(registry)
	if err != nil {
		t.Fatal(err)
	}

	subjects := map[string]segment.Segment{}
	isLaidOut := map[string]bool{}
	for position, instances := range layout {
		for at, instance := range instances {
			named, isNamed := instance.(segment.Instance)
			if !isNamed {
				t.Fatalf("expected a named instance in the default layout, got %T", instance)
			}
			subjects[fmt.Sprintf("%s at %d.%d", named.Name, position, at)] = named.Segment
			isLaidOut[named.Name] = true
		}
	}

	for name, factory := range registry {
		if isLaidOut[name] {
			continue
		}

		instance, err := factory(infoOptions{})
		if err != nil {
			t.Fatalf("%s cannot be built without options: %v", name, err)
		}
		subjects[name] = instance
	}

	return subjects
}

func drawsOverSpan(instance segment.Segment, isRunning bool) (int, bool) {
	refresher, isRefresher := instance.(segment.Refresher)
	if !isRefresher {
		return 0, false
	}

	draws := 0
	for endsAt := time.Now().Add(refreshSpan); time.Now().Before(endsAt); {
		segment.LadderOf(instance, segment.Context{})
		draws++

		dueAt := refresher.NextRefresh(segment.Phase{At: time.Now(), IsRunning: isRunning})
		if dueAt.IsZero() {
			break
		}

		time.Sleep(max(time.Until(dueAt), soonestRefresh))
	}

	return draws, true
}

func refreshRegistry(workspaceDir string, world refreshWorld) segment.Registry {
	return NewRegistry(Options{
		Workspace: work.At(workspaceDir),
		Session:   cycle.Session{Name: "tame-impala", Model: "claude-haiku-5-5", Effort: "high"},
		Sources: Sources{
			IsTurnRunning:      func() bool { return world.isRunning },
			IsSessionPersisted: func() bool { return true },
			GetContextUsage:    func() (int, int) { return 48_000, 1_000_000 },
			GetCacheUsage:      func() (int, int) { return 91, 100 },
			GetSessionSpend:    func() (float64, bool) { return 0.01, true },
			GetGrantedCaps:     func() caps.Set { var granted caps.Set; return granted },
			GetGroupStatus:     func() caps.GroupStatus { return caps.GroupStatus{} },
			GetPathGrants:      func() []pathgrant.Grant { return nil },
			GetForwardedRoutes: func() []portgrant.Route { return nil },
			IsPrefixPending:    func() bool { return false },
			GetTurnTiming:      func() turn.Timing { return turn.Timing{UserTurn: time.Minute, ModelTurn: time.Second} },
			GetTurnCount:       func() int { return 6 },
			GetJobs:            world.getJobs,
		},
	})
}

func plainWorkspace(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}

func repositoryWorkspace(t *testing.T) string {
	t.Helper()

	workspaceDir := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", "-q", "-b", "main")
	command.Dir = workspaceDir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("failed to create the repository: %s\n%s", err, output)
	}

	return workspaceDir
}

func brokenRepositoryWorkspace(t *testing.T) string {
	t.Helper()

	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, ".git"), []byte("gitdir: /nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
}
