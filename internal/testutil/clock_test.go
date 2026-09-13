package testutil

import (
	"sync"
	"testing"
	"time"
)

func TestClockDeterministic(test *testing.T) {
	start := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.FixedZone("fixture", 9*60*60))
	clock := NewClock(start)
	if actual := clock.Now(); actual != start {
		test.Fatalf("Now() = %v, want %v", actual, start)
	}
	clock.Advance(90 * time.Second)
	clock.Advance(-30 * time.Second)
	clock.Advance(0)
	expected := start.Add(time.Minute)
	if actual := clock.Now(); actual != expected {
		test.Fatalf("Now() after advances = %v, want %v", actual, expected)
	}

	const workers = 12
	const advances = 80
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for range advances {
				clock.Advance(time.Nanosecond)
				clock.Now()
			}
		})
	}
	group.Wait()
	expected = expected.Add(workers * advances * time.Nanosecond)
	if actual := clock.Now(); actual != expected {
		test.Errorf("Now() after concurrent advances = %v, want %v", actual, expected)
	}
	if actual := NewClock(time.Time{}).Now(); !actual.IsZero() {
		test.Errorf("zero clock = %v, want zero time", actual)
	}
}
