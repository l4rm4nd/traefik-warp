package providers

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

const FetchTimeout = 10 * time.Second

var HTTPClient = &http.Client{Timeout: FetchTimeout}

var ErrNoList = errors.New("provider IP list not fetched yet")

// ListCache shares one provider IP list between all middleware instances of the process.
// A failed refresh keeps the last good list.
type ListCache struct {
	Fetch      func() ([]string, error)
	TTL        time.Duration
	RetryAfter time.Duration

	mu        sync.Mutex
	list      []string
	fetchedAt time.Time
	failedAt  time.Time
}

func (c *ListCache) Get(now time.Time) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.list) > 0 && now.Sub(c.fetchedAt) < c.TTL {
		return c.copyList(), nil
	}
	if !c.failedAt.IsZero() && now.Sub(c.failedAt) < c.RetryAfter {
		return c.stale()
	}

	list, err := c.Fetch()
	if err != nil || len(list) == 0 {
		c.failedAt = now
		return c.stale()
	}
	c.list = list
	c.fetchedAt = now
	c.failedAt = time.Time{}
	return c.copyList(), nil
}

func (c *ListCache) stale() ([]string, error) {
	if len(c.list) == 0 {
		return nil, ErrNoList
	}
	return c.copyList(), nil
}

func (c *ListCache) copyList() []string {
	return append([]string(nil), c.list...)
}
