package usage

import (
	"time"

	"crdx.org/io/internal/state"
	"crdx.org/io/pkg/agent"
)

type cache struct {
	Version   int                 `json:"version"`
	FetchedAt time.Time           `json:"fetched_at"`
	Windows   []agent.UsageWindow `json:"windows"`
	Probe     *probeState         `json:"probe,omitempty"`
}

type probeState struct {
	AttemptedAt time.Time `json:"attempted_at"`
	NextAt      time.Time `json:"next_at"`
	Failures    int       `json:"failures,omitempty"`
	Failure     string    `json:"failure,omitempty"`
}

type cacheStore struct {
	path string
}

func newCacheStore(path string) cacheStore {
	return cacheStore{path: path}
}

func (self cacheStore) read() cache {
	var storedCache cache

	if err := state.Read(self.path, cacheFormat, &storedCache); err != nil {
		return cache{}
	}

	return storedCache
}

func (self cacheStore) tryUpdate(update func(*cache) error) (bool, error) {
	return state.TryUpdate(self.path, cacheFormat, update)
}

func (self cacheStore) update(update func(*cache) error) error {
	return state.Update(self.path, cacheFormat, update)
}
