# Semantic stream timing integration acceptance

## Scope and baseline

This batch applies the reviewed timing/delivery patches to production source
`5825ebd67`, preserving the deployed changes absent from `origin/main`.
The production backup `fix.patch` exactly matches `git diff ddb1e1fd6 5825ebd67`.
Merging current main `b38533736` into that baseline changes no source bytes.
Existing user modifications in the original checkout were not used or edited.

The timing observer records actual nonempty text, thinking and tool content after
the SSE helper writes and flushes. A protocol start event, signature or empty
delta does not produce a semantic TTFT. Upstream event latency remains available
separately. Unknown formats and legacy rows are labeled explicitly. Server flush
timing is not confirmation that a remote client has rendered content.

Stream failures propagate as skip-retry errors, stop later success terminals and
retain partial usage on cancellation. The native Chat path flushes content
immediately instead of holding a chunk until another event arrives. Normal
payload, token accounting, model names and signature bytes are unchanged.

## Fresh local acceptance

| Check | Result |
| --- | --- |
| Root `go test -json ./...` | 1,785 test/subtest passes; zero failures; 4 configured database cases skipped |
| Root `go vet ./...`, `go build ./...` | Passed |
| Independent `relaykit` build and tests with `GOWORK=off` | Passed |
| Focused Go race tests | 145 test/subtest passes; zero failures |
| Full web suite | 60 files, 362 tests passed |
| Web typecheck, touched-file lint/format and build | Passed |
| Native Chat private replay | 6/6 payload and usage byte-identical to recorded baseline |
| Native Messages/Responses private replay | 6/6 payload and usage byte-identical |
| Converted private replay | 6/6 payload equivalent after documented generated ID/timestamp normalization; usage byte-identical |
| Final integration manifest source hashes | 42/42 archived expected hashes matched |

The four Go skips require independent MySQL/PostgreSQL migration environments.
They were not claimed as executed. No database code is changed by this batch.

The first full UI invocation ran under Node 26 with experimental Web Storage,
which shadowed jsdom storage and caused failures/hanging workers. That run was
stopped and retained. With `NODE_OPTIONS=--no-experimental-webstorage`, the entire
suite passed without source changes:

```sh
NODE_OPTIONS=--no-experimental-webstorage bun run test --maxWorkers=4
```

Private capture fixtures were installed only for replay and removed afterward.
No raw captures, credentials, conversations, or signed reasoning are committed.
Local diagnostic logs remain in `/tmp/newapi-stream-release-20261101`.

## Release limits

This document records local acceptance, not a production rollout. GitHub CI,
image provenance/digest verification, candidate health, traffic switching and
post-deployment correlation must be recorded before calling it deployed.

Kiro remains at the separately verified `6ddd945` release with CCTest report
`830b7867-465d-4494-81f7-05b89ee7e3c4` (100%). These NewAPI tests do not establish
new platform scores, universal 1–2 second generation latency, full-model identity
suppression, or the unreplicated standalone period report.
