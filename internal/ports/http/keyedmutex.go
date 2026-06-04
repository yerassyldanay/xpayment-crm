package http

import "sync"

// keyedMutex serializes work per key (per contact), so two near-simultaneous
// messages on the same contact don't read-merge-write the profile and clobber
// each other (docs/06 · concurrency).
type keyedMutex struct {
	mu sync.Mutex
	m  map[int64]*sync.Mutex
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{m: map[int64]*sync.Mutex{}} }

func (k *keyedMutex) Lock(key int64) func() {
	k.mu.Lock()
	mu, ok := k.m[key]
	if !ok {
		mu = &sync.Mutex{}
		k.m[key] = mu
	}
	k.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}
