package providers

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	FetchTimeout = 10 * time.Second
	ListTTL      = time.Hour
)

var RetryInterval = 30 * time.Second

var HTTPClient = &http.Client{Timeout: FetchTimeout}

var ErrNoList = errors.New("provider IP list not fetched yet")

// ListCache shares one provider IP list between all middleware instances of the process.
// Only the first download blocks; later refreshes run in the background.
type ListCache struct {
	Fetch func() ([]string, error)
	TTL   time.Duration

	mu         sync.Mutex
	list       []string
	fetchedAt  time.Time
	failedAt   time.Time
	lastErr    error
	refreshing bool
}

func (c *ListCache) Get(now time.Time) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.list) > 0 {
		if now.Sub(c.fetchedAt) >= c.TTL && !c.refreshing && !c.recentlyFailed(now) {
			c.refreshing = true
			go c.refresh(now)
		}
		return c.copyList(), nil
	}

	if c.recentlyFailed(now) {
		return nil, c.noListErr()
	}
	// Held during the first download so the other instances wait for it instead of fetching too.
	list, err := c.fetch()
	if err != nil {
		c.failedAt = now
		c.lastErr = err
		return nil, c.noListErr()
	}
	c.store(list, now)
	return c.copyList(), nil
}

func (c *ListCache) refresh(now time.Time) {
	list, err := c.fetch()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	if err != nil {
		c.failedAt = now
		c.lastErr = err
		return
	}
	c.store(list, now)
}

func (c *ListCache) fetch() ([]string, error) {
	list, err := c.Fetch()
	if err == nil && len(list) == 0 {
		err = errors.New("empty list")
	}
	return list, err
}

func (c *ListCache) store(list []string, now time.Time) {
	c.list = list
	c.fetchedAt = now
	// Separate assignments: Yaegi panics on `a, b = x, nil` with an interface field.
	c.failedAt = time.Time{}
	c.lastErr = nil
}

func (c *ListCache) recentlyFailed(now time.Time) bool {
	return !c.failedAt.IsZero() && now.Sub(c.failedAt) < RetryInterval
}

func (c *ListCache) noListErr() error {
	if c.lastErr == nil {
		return ErrNoList
	}
	return fmt.Errorf("%w: %v", ErrNoList, c.lastErr)
}

func (c *ListCache) copyList() []string {
	return append([]string(nil), c.list...)
}
