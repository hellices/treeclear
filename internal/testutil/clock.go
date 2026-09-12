package testutil

import (
	"sync"
	"time"
)

type Clock struct {
	mutex sync.RWMutex
	now   time.Time
}

func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

func (clock *Clock) Now() time.Time {
	clock.mutex.RLock()
	defer clock.mutex.RUnlock()
	return clock.now
}

func (clock *Clock) Advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}
