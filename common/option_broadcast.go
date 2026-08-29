package common

// option_broadcast — cross-node in-memory config sync via Redis pub/sub.
//
// Problem this solves
// -------------------
// Every process holds its own copy of options in `common.OptionMap` plus
// registered config structs (e.g. `EconomySetting`). When one process
// receives PUT /api/option/, it writes the DB row and updates its own
// memory, but a sibling process (blue vs green, or n-of-cluster) never
// re-reads the row — its memory stays stale until the next restart.
//
// The visible symptom is Caddy's LB dispatching the same admin request
// to blue (up-to-date) and green (stale) in alternating requests, so
// running `GET /api/option/` back-to-back returns different values.
// Round 5 of the v4 economy rollout hit this directly: `disabled_tiers`
// took effect on blue immediately, but green kept serving the old
// `tier_cards` until a manual `docker compose restart`.
//
// Solution
// --------
// Every process subscribes to a single Redis channel. When any process
// writes an option, it publishes the (key, value) plus a node ID that
// identifies the origin process. Subscribers apply the value to their
// local memory, but skip messages that originated in the same process
// (both to avoid double-applying inside the publisher and to keep the
// loop breaker obvious).
//
// If Redis isn't configured (single-node dev / test), the publish is a
// no-op and the subscriber never starts. Callers must still update
// their own memory locally after the DB write; this module only
// propagates to *other* processes.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

// optionUpdateChannel is the Redis pub/sub channel every node listens on.
// The prefix is deliberately verbose so it can't collide with keys the
// application uses for caching or rate-limit counters.
const optionUpdateChannel = "newapi:v1:option-update"

// optionUpdateMessage is what's sent over the wire. Value is stored as a
// raw string exactly the way `UpdateOption` receives it — the
// subscriber pipes it back through `updateOptionMap`, which owns the
// reflect-based type dispatch. Keep this struct additive so nodes on
// slightly different versions can coexist.
type optionUpdateMessage struct {
	NodeID string `json:"node_id"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// nodeID is a per-process identifier used to filter self-published
// messages. Generated once at import time so it survives across
// InitRedisClient timing.
var nodeID = generateNodeID()

func generateNodeID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Falls back to a timestamp so collisions across a fleet are still
		// astronomically unlikely — nodeID doesn't need cryptographic
		// strength, it only has to be unique per running process.
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

// NodeID returns this process's broadcast identifier. Exposed so tests
// and diagnostics endpoints can surface it (e.g. to confirm two
// containers actually have distinct IDs during a rollout).
func NodeID() string { return nodeID }

// PublishOptionUpdate broadcasts (key, value) to every node on the same
// Redis instance. Callers MUST have already updated their own local
// state (DB row + OptionMap) before calling — the point of the message
// is to propagate to *other* nodes.
//
// Errors are logged but not returned in a way that fails the write:
// losing a broadcast means the fleet is temporarily inconsistent, not
// broken, and the offending admin call has already persisted to DB.
// Missing broadcast will heal on the next process restart.
func PublishOptionUpdate(key, value string) {
	if !RedisEnabled || RDB == nil {
		return
	}
	msg := optionUpdateMessage{
		NodeID: nodeID,
		Key:    key,
		Value:  value,
	}
	payload, err := json.Marshal(&msg)
	if err != nil {
		SysError("option-broadcast: marshal failed: " + err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := RDB.Publish(ctx, optionUpdateChannel, payload).Err(); err != nil {
		SysError("option-broadcast: publish failed: " + err.Error())
	}
}

// StartOptionUpdateSubscriber starts a goroutine that listens on the
// broadcast channel and applies incoming updates via `apply`. Messages
// from this process's own `nodeID` are ignored so the publisher never
// double-applies its own write.
//
// `apply` should be `model.applyRemoteOptionUpdate` — a thin wrapper
// around `updateOptionMap` that skips the DB write leg. Passing it as
// a function lets this package live in `common/` without pulling in the
// `model` import cycle.
//
// Returns immediately after starting the goroutine. Safe to call once
// per process; call sites should live right after `InitRedisClient`.
func StartOptionUpdateSubscriber(apply func(key, value string) error) {
	if !RedisEnabled || RDB == nil {
		SysLog("option-broadcast: Redis disabled, cross-node option sync is off")
		return
	}
	if apply == nil {
		SysError("option-broadcast: apply func is nil, subscriber not started")
		return
	}
	sub := RDB.Subscribe(context.Background(), optionUpdateChannel)
	SysLog("option-broadcast: subscribed to " + optionUpdateChannel + " as node=" + nodeID)
	go runOptionUpdateLoop(sub, apply)
}

// runOptionUpdateLoop is the receive loop for StartOptionUpdateSubscriber.
// Extracted so the tests can drive it with a synthetic PubSub.
func runOptionUpdateLoop(sub *redis.PubSub, apply func(key, value string) error) {
	defer sub.Close()
	ch := sub.Channel()
	for msg := range ch {
		var evt optionUpdateMessage
		if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
			SysError("option-broadcast: bad payload: " + err.Error())
			continue
		}
		if evt.NodeID == nodeID {
			// Self-echo: our own publish came back on the same channel.
			// The publisher already updated local memory, so re-applying
			// would be a no-op but the skip keeps the log clean.
			continue
		}
		if err := apply(evt.Key, evt.Value); err != nil {
			SysError("option-broadcast: apply " + evt.Key + " failed: " + err.Error())
		}
	}
}
