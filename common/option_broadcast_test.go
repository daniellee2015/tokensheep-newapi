package common

// option_broadcast_test — locks the two invariants that keep the
// cross-node option-sync loop from becoming a footgun.
//
// (1) A message we publish ourselves must NOT be re-applied locally.
//     If it were, every UpdateOption call would run updateOptionMap
//     twice, which is idempotent for setters but doubles up on side-
//     effect hooks (e.g. cache invalidation) and hides bugs where a
//     handler wrongly assumes it's called once per write.
//
// (2) A message from another node MUST land through the supplied apply
//     func. If it doesn't, the whole point of the broadcast is void —
//     siblings never converge and Round 5's blue-green drift returns.
//
// The tests drive runOptionUpdateLoop directly with a synthetic
// *redis.PubSub so they don't need a live Redis. That keeps them fast
// and lets CI run them without an external service.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBroadcastRedis stands up an in-process miniredis, points RDB at
// it, and returns a cleanup that restores the previous global state.
// Broadcast state (nodeID, RedisEnabled) is process-global here just
// like in production, so tests coordinate via t.Cleanup instead of
// racing on shared state.
func setupBroadcastRedis(t *testing.T) (*miniredis.Miniredis, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	prevRDB := RDB
	prevEnabled := RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	RedisEnabled = true
	return mr, func() {
		_ = RDB.Close()
		RDB = prevRDB
		RedisEnabled = prevEnabled
	}
}

// waitFor polls cond up to timeout, sleeping poll between checks.
// Miniredis's Publish deliveries are prompt but not synchronous; this
// avoids flaky tests without hard sleeps.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestPublishOptionUpdate_SelfEchoIgnored(t *testing.T) {
	_, cleanup := setupBroadcastRedis(t)
	defer cleanup()

	var applied atomic.Int32
	sub := RDB.Subscribe(context.Background(), optionUpdateChannel)
	// runOptionUpdateLoop closes sub on return; start it in a goroutine
	// and stop by closing the RDB (which closes the underlying conn).
	done := make(chan struct{})
	go func() {
		defer close(done)
		runOptionUpdateLoop(sub, func(key, value string) error {
			applied.Add(1)
			return nil
		})
	}()

	// Publish with our own nodeID — apply must NOT be called.
	PublishOptionUpdate("TokenSheepEconomy.disabled_tiers", `{"vip":true}`)

	// Give the subscriber a chance to receive the self-echo and filter it.
	time.Sleep(50 * time.Millisecond)
	require.EqualValues(t, 0, applied.Load(),
		"self-published message must be filtered out by NodeID match")

	_ = sub.Close()
	<-done
}

func TestPublishOptionUpdate_ForeignMessageApplied(t *testing.T) {
	_, cleanup := setupBroadcastRedis(t)
	defer cleanup()

	var (
		mu         sync.Mutex
		lastKey    string
		lastValue  string
		applyCount int
	)
	sub := RDB.Subscribe(context.Background(), optionUpdateChannel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runOptionUpdateLoop(sub, func(key, value string) error {
			mu.Lock()
			lastKey = key
			lastValue = value
			applyCount++
			mu.Unlock()
			return nil
		})
	}()

	// Publish through a raw Redis call with a *different* NodeID so the
	// receiver treats it as originating from another process. This
	// mirrors the runtime flow where two containers share Redis and each
	// runs its own subscriber loop.
	require.NoError(t, RDB.Publish(context.Background(), optionUpdateChannel,
		`{"node_id":"OTHER_NODE","key":"GroupRatio","value":"{\"default\":1}"}`,
	).Err())

	ok := waitFor(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return applyCount == 1
	})
	assert.True(t, ok, "foreign message must be applied exactly once")

	mu.Lock()
	assert.Equal(t, "GroupRatio", lastKey)
	assert.Equal(t, `{"default":1}`, lastValue)
	mu.Unlock()

	_ = sub.Close()
	<-done
}

func TestPublishOptionUpdate_DisabledRedisIsNoop(t *testing.T) {
	prevEnabled := RedisEnabled
	prevRDB := RDB
	RedisEnabled = false
	RDB = nil
	defer func() {
		RedisEnabled = prevEnabled
		RDB = prevRDB
	}()

	// No panic, no error, no side effect. If PublishOptionUpdate ever
	// forgets its RedisEnabled guard and dereferences RDB we'd panic here.
	PublishOptionUpdate("SomeKey", "some-value")

	var called int32
	StartOptionUpdateSubscriber(func(key, value string) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	assert.EqualValues(t, 0, atomic.LoadInt32(&called),
		"subscriber must not fire when Redis is disabled")
}
