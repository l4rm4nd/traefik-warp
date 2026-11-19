package providers

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeFetch struct {
	calls   int
	results [][]string
	errs    []error
}

func (f *fakeFetch) fetch() ([]string, error) {
	i := f.calls
	f.calls++
	return f.results[i], f.errs[i]
}

func newCache(f *fakeFetch) *ListCache {
	return &ListCache{Fetch: f.fetch, TTL: time.Hour, RetryAfter: 30 * time.Second}
}

var t0 = time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC)

func TestFreshListIsFetchedOnce(t *testing.T) {
	f := &fakeFetch{results: [][]string{{"173.245.48.0/20"}}, errs: []error{nil}}
	c := newCache(f)

	for i := 0; i < 38; i++ {
		got, err := c.Get(t0.Add(time.Duration(i) * time.Second))
		if err != nil || !reflect.DeepEqual(got, []string{"173.245.48.0/20"}) {
			t.Fatalf("call %d: got %v, %v", i, got, err)
		}
	}
	if f.calls != 1 {
		t.Fatalf("fetched %d times, want 1", f.calls)
	}
}

func TestStaleListIsFetchedAgain(t *testing.T) {
	f := &fakeFetch{results: [][]string{{"a"}, {"b"}}, errs: []error{nil, nil}}
	c := newCache(f)

	c.Get(t0)
	got, _ := c.Get(t0.Add(time.Hour))

	if f.calls != 2 || !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("calls %d, got %v; want a refetch returning b", f.calls, got)
	}
}

func TestFailedRefreshKeepsTheLastGoodList(t *testing.T) {
	f := &fakeFetch{results: [][]string{{"a"}, nil}, errs: []error{nil, errors.New("down")}}
	c := newCache(f)

	c.Get(t0)
	got, err := c.Get(t0.Add(2 * time.Hour))

	if err != nil || !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("got %v, %v; want the last good list", got, err)
	}
}

func TestFailureWithoutAListIsNotRetriedBeforeRetryAfter(t *testing.T) {
	f := &fakeFetch{results: [][]string{nil, {"a"}}, errs: []error{errors.New("down"), nil}}
	c := newCache(f)

	if _, err := c.Get(t0); !errors.Is(err, ErrNoList) {
		t.Fatalf("first call: err %v, want ErrNoList", err)
	}
	if _, err := c.Get(t0.Add(10 * time.Second)); !errors.Is(err, ErrNoList) || f.calls != 1 {
		t.Fatalf("within RetryAfter: err %v, calls %d; want ErrNoList without a fetch", err, f.calls)
	}
	got, err := c.Get(t0.Add(30 * time.Second))
	if err != nil || !reflect.DeepEqual(got, []string{"a"}) || f.calls != 2 {
		t.Fatalf("after RetryAfter: got %v, %v, calls %d; want a refetch", got, err, f.calls)
	}
}

func TestEmptyListCountsAsFailure(t *testing.T) {
	f := &fakeFetch{results: [][]string{{}}, errs: []error{nil}}
	c := newCache(f)

	if _, err := c.Get(t0); !errors.Is(err, ErrNoList) {
		t.Fatalf("err %v, want ErrNoList", err)
	}
}

func TestCallerCannotChangeTheCachedList(t *testing.T) {
	f := &fakeFetch{results: [][]string{{"a"}}, errs: []error{nil}}
	c := newCache(f)

	got, _ := c.Get(t0)
	got[0] = "changed"
	again, _ := c.Get(t0)

	if again[0] != "a" {
		t.Fatalf("cached list changed to %v", again)
	}
}
