package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useChannelConcurrencyMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	})
	return server
}

func TestChannelConcurrencyIsSharedAndReleased(t *testing.T) {
	server := useChannelConcurrencyMiniRedis(t)

	releaseFirst, err := AcquireChannelConcurrency(context.Background(), 96, 1)
	require.NoError(t, err)
	active, err := server.Get(channelConcurrencyPrefix + "96")
	require.NoError(t, err)
	assert.Equal(t, "1", active)

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = AcquireChannelConcurrency(waitCtx, 96, 1)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	releaseFirst()
	assert.False(t, server.Exists(channelConcurrencyPrefix+"96"))

	releaseSecond, err := AcquireChannelConcurrency(context.Background(), 96, 1)
	require.NoError(t, err)
	releaseSecond()
}

func TestChannelConcurrencyIsolatedByChannel(t *testing.T) {
	useChannelConcurrencyMiniRedis(t)

	release96, err := AcquireChannelConcurrency(context.Background(), 96, 1)
	require.NoError(t, err)
	defer release96()

	release97, err := AcquireChannelConcurrency(context.Background(), 97, 1)
	require.NoError(t, err)
	release97()
}

func TestChannelConcurrencyZeroDisablesLimit(t *testing.T) {
	release, err := AcquireChannelConcurrency(context.Background(), 96, 0)
	require.NoError(t, err)
	release()
}
