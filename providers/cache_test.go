package providers

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type step struct {
	list []string
	err  error
	gate chan struct{} // if set, the fetch waits for it to be closed
}

type fakeFetch struct {
	mu    sync.Mutex
	steps []step
	calls int
	done  chan struct{}
}

func newFake(steps ...step) *fakeFetch {
	return &fakeFetch{steps: steps, done: make(chan struct{}, len(steps))}
}

func (f *fakeFetch) fetch() ([]string, error) {
	f.mu.Lock()
	s := f.steps[f.calls]
	f.calls++
	f.mu.Unlock()

	if s.gate != nil {
		<-s.gate
	}
	f.done <- struct{}{}
	return s.list, s.err
}

func (f *fakeFetch) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFetch) waitCalls(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.callCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("fetch %d did not start", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fakeFetch) waitFetches(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("fetch %d did not finish", i+1)
		}
	}
}

// waitRefreshStored waits until the background refresh has released the cache.
func waitRefreshStored(c *ListCache) {
	for {
		c.mu.Lock()
		busy := c.refreshing
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

var t0 = time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC)

func newCache(f *fakeFetch) *ListCache {
	return &ListCache{Fetch: f.fetch, TTL: time.Hour}
}

func TestFreshListIsFetchedOnce(t *testing.T) {
	f := newFake(step{list: []string{"173.245.48.0/20"}})
	c := newCache(f)

	for i := 0; i < 38; i++ {
		got, err := c.Get(t0.Add(time.Duration(i) * time.Second))
		if err != nil || !reflect.DeepEqual(got, []string{"173.245.48.0/20"}) {
			t.Fatalf("call %d: got %v, %v", i, got, err)
		}
	}
	if f.callCount() != 1 {
		t.Fatalf("fetched %d times, want 1", f.callCount())
	}
}

func TestStaleListIsServedWhileRefreshing(t *testing.T) {
	gate := make(chan struct{})
	f := newFake(step{list: []string{"a"}}, step{list: []string{"b"}, gate: gate})
	c := newCache(f)
	c.Get(t0)

	got, err := c.Get(t0.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("while refreshing: got %v, %v; want the old list without waiting", got, err)
	}
	f.waitCalls(t, 2)
	c.Get(t0.Add(time.Hour + time.Second))
	if f.callCount() != 2 {
		t.Fatalf("fetched %d times, want one refresh in flight", f.callCount())
	}

	close(gate)
	f.waitFetches(t, 2)
	waitRefreshStored(c)

	got, _ = c.Get(t0.Add(time.Hour + 2*time.Second))
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("after refresh: got %v, want b", got)
	}
}

func TestFailedRefreshKeepsTheLastGoodList(t *testing.T) {
	f := newFake(step{list: []string{"a"}}, step{err: errors.New("down")})
	c := newCache(f)
	c.Get(t0)

	c.Get(t0.Add(2 * time.Hour))
	f.waitFetches(t, 2)
	waitRefreshStored(c)

	got, err := c.Get(t0.Add(2*time.Hour + time.Second))
	if err != nil || !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("got %v, %v; want the last good list", got, err)
	}
	if f.callCount() != 2 {
		t.Fatalf("fetched %d times; a failed refresh must wait RetryInterval", f.callCount())
	}
}

func TestFailureWithoutAListIsNotRetriedBeforeRetryInterval(t *testing.T) {
	f := newFake(step{err: errors.New("down")}, step{list: []string{"a"}})
	c := newCache(f)

	_, err := c.Get(t0)
	if !errors.Is(err, ErrNoList) || err.Error() != "provider IP list not fetched yet: down" {
		t.Fatalf("first call: err %v, want ErrNoList with the cause", err)
	}
	if _, err := c.Get(t0.Add(RetryInterval - time.Second)); !errors.Is(err, ErrNoList) || f.callCount() != 1 {
		t.Fatalf("within RetryInterval: err %v, calls %d; want ErrNoList without a fetch", err, f.callCount())
	}
	got, err := c.Get(t0.Add(RetryInterval))
	if err != nil || !reflect.DeepEqual(got, []string{"a"}) || f.callCount() != 2 {
		t.Fatalf("after RetryInterval: got %v, %v, calls %d; want a refetch", got, err, f.callCount())
	}
}

func TestEmptyListCountsAsFailure(t *testing.T) {
	f := newFake(step{list: []string{}})
	c := newCache(f)

	if _, err := c.Get(t0); !errors.Is(err, ErrNoList) {
		t.Fatalf("err %v, want ErrNoList", err)
	}
}

func TestCallerCannotChangeTheCachedList(t *testing.T) {
	f := newFake(step{list: []string{"a"}})
	c := newCache(f)

	got, _ := c.Get(t0)
	got[0] = "changed"
	again, _ := c.Get(t0)

	if again[0] != "a" {
		t.Fatalf("cached list changed to %v", again)
	}
}
