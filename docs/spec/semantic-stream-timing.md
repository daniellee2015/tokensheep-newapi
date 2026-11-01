# Streaming timing and delivery reliability

## Goal and release baseline

Show the time until real downstream content instead of presenting an upstream
protocol event as TTFT. Stop delivery and retries correctly when the client
disconnects or a stream write fails.

Production on 2026-11-01 is image `20261030-ratelimit-billing-fallback`, image ID
`sha256:9d6f34f4716cdac2a7da96f49b486492b4b5a175be3cfa8d9664282d34fc4d9e`.
Its recorded source is `ddb1e1fd6` plus the byte-identical production fixes in
`aa60b4ffb` and `5825ebd67`. This branch preserves that full baseline. Merging
`origin/main` (`b38533736`) into it has zero source changes. Main is missing
previously deployed commits; releasing main plus only the timing patch would
remove existing pricing, fallback and log features.

## Scope

Integrate the six already-tested Kiro integration patches, in order: semantic
first content, upstream request ID, native Chat flush, native SSE write failures,
converted stream write failures, and timing UI. Their archived fixed base is
`ddb1e1fd6`; verify applicability and test again on the production baseline.

Expose separate event/text/thinking/tool times. Known streams without content
show no content; legacy and unknown timing is explicitly labeled. Native Chat
flushes content as it arrives. Stream write failures stop later success events
and retries; cancellation preserves already-reported accounting usage.

## Non-goals

No model, prompt, signature, token usage, pricing, group or routing policy
changes. No Kiro release. No punctuation stripping. No claim of faster model
generation, universal 1–2 second TTFT, full identity suppression, or repaired
third-party scores. Preserve existing source checkout modifications.

## Acceptance

- Existing deterministic content timing, write failure, cancellation, request
  correlation and native/converted stream regressions pass on this baseline.
- Required Go vet/build/tests and independent relaykit checks pass.
- Usage-log UI regression tests, typecheck, touched-file lint/format and build
  pass. Keep the previous rendered desktop/mobile evidence.
- Existing real SSE replay fixtures preserve native payload and usage; converted
  replay normalizes only generated IDs/timestamps. No credentials or customer
  content enter Git.
- After authorized rollout, correlate a bounded real request with both NewAPI
  and Kiro traces; verify content timing and a tool continuation.

## Risks, deployment and rollback

This changes shared stream helpers, so native and converted paths both require
checks. Server write/flush timing is not proof of client-screen rendering.
Old logs are not rewritten. No schema migration or configuration change is
required by this batch. Production baseline history includes older migrations
already applied in production.

Use PR/main and GitHub Actions image only. Verify image revision and digest,
pull a candidate, health-check, route gradually and drain active streams before
stopping an old instance. Preserve live configuration and database. Roll back
traffic to the retained image on failures; never restore an old database over
new user activity.
