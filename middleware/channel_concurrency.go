package middleware

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelConcurrencyPrefix       = "ts:channel:active:"
	channelConcurrencyTTL          = 15 * time.Minute
	channelConcurrencyPollInterval = 200 * time.Millisecond
	channelConcurrencyMaxWait      = 60 * time.Second
)

var (
	ErrChannelConcurrencyWait = errors.New("channel concurrency wait limit reached")
	channelConcurrencyMemory  = struct {
		sync.Mutex
		active map[int]int
	}{active: make(map[int]int)}
)

// AcquireChannelConcurrency waits for a channel-level in-flight slot. The
// Redis counter is shared by all application replicas; Redis-less deployments
// use a process-local fallback. A zero limit disables the gate.
func AcquireChannelConcurrency(ctx context.Context, channelID int, limit int) (func(), error) {
	if channelID <= 0 || limit <= 0 {
		return func() {}, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, channelConcurrencyMaxWait)
	defer cancel()

	for {
		var (
			release  func()
			acquired bool
		)
		if common.RedisEnabled && common.RDB != nil {
			var err error
			release, acquired, err = acquireChannelConcurrencyRedis(waitCtx, channelID, limit)
			if err != nil {
				release, acquired = acquireChannelConcurrencyMemory(channelID, limit)
			}
		} else {
			release, acquired = acquireChannelConcurrencyMemory(channelID, limit)
		}
		if acquired {
			return release, nil
		}

		timer := time.NewTimer(channelConcurrencyPollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrChannelConcurrencyWait
		case <-timer.C:
		}
	}
}

func acquireChannelConcurrencyRedis(ctx context.Context, channelID int, limit int) (func(), bool, error) {
	key := channelConcurrencyPrefix + strconv.Itoa(channelID)
	acquireScript := `
		local count = redis.call('INCR', KEYS[1])
		if count > tonumber(ARGV[1]) then
			redis.call('DECR', KEYS[1])
			redis.call('EXPIRE', KEYS[1], ARGV[2])
			return 0
		end
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`
	result, err := common.RDB.Eval(
		ctx,
		acquireScript,
		[]string{key},
		limit,
		int(channelConcurrencyTTL.Seconds()),
	).Int()
	if err != nil {
		return nil, false, err
	}
	if result != 1 {
		return nil, false, nil
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseScript := `
				local count = redis.call('DECR', KEYS[1])
				if count <= 0 then
					redis.call('DEL', KEYS[1])
				end
				return count
			`
			_, _ = common.RDB.Eval(context.Background(), releaseScript, []string{key}).Result()
		})
	}, true, nil
}

func acquireChannelConcurrencyMemory(channelID int, limit int) (func(), bool) {
	channelConcurrencyMemory.Lock()
	defer channelConcurrencyMemory.Unlock()
	if channelConcurrencyMemory.active[channelID] >= limit {
		return nil, false
	}
	channelConcurrencyMemory.active[channelID]++

	var once sync.Once
	return func() {
		once.Do(func() {
			channelConcurrencyMemory.Lock()
			defer channelConcurrencyMemory.Unlock()
			if channelConcurrencyMemory.active[channelID] > 1 {
				channelConcurrencyMemory.active[channelID]--
			} else {
				delete(channelConcurrencyMemory.active, channelID)
			}
		})
	}, true
}
