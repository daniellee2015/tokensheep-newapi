# 2026-09-25 CPA validation 403 storm and stale WebUI

## Scope

This record covers the September 25 incident involving VPS196 CPA, the
`cpa-daemon-v4` account pool manager, and the auth-file quota UI. Times quoted
from CPA application logs use Asia/Shanghai. Daemon service timestamps use the
server timezone (`+0200`).

No load test was used during the investigation. Counts came from existing CPA,
new-api, and daemon logs. Quota endpoints were not refreshed manually.

## Observed traffic

By approximately 17:30 server time, the two new-api instances had recorded
39,332 relay requests for the day. Their combined relay responses included
about 20,513 status 503 responses, 2,085 status 429 responses, 350 status 400
responses, and 8 status 404 responses. The largest failure window began around
13:00 server time, and 503 responses dominated after 15:00.

CPA blue handled inference and management traffic; CPA green was effectively
standby and logged health checks only. CPA blue recorded:

- 45,731 Antigravity credential selections;
- 25,027 upstream execution failures with status 403;
- 979 upstream execution failures with status 429;
- 1 upstream execution failure with status 503;
- 693 short-cooldown messages and 1,195 auth-unavailable messages.

The 403 failures were concentrated on a small number of credentials. Three
credentials each received about 6,000 failed attempts, another received 3,364,
and another received 2,612. The upstream body was `PERMISSION_DENIED` with
`VALIDATION_REQUIRED` / `Verify your account to continue.`

This was not a per-request retry explosion inside CPA. Of the request IDs with
an Antigravity credential selection, 45,730 selected one credential and only
one selected two. The storm came from later requests repeatedly selecting the
same validation-blocked credentials.

## Management API traffic

The daemon queried roughly 97 Antigravity credentials every 30 minutes. This
produced about 194 successful `POST /v0/management/api-call` requests per hour.
Those calls read quota state; they are not model generation calls and do not
explain model quota consumption.

Additional bursts of management `api-call` and `auth-files/status` requests
matched interactive quota refreshes and batch enable/disable actions. They
increased quota endpoint traffic, but the inference log showed that the main
failure storm was repeated model traffic against credentials already returning
validation-required 403 responses.

## Root causes

### Validation-required credentials remained selectable

Production had both:

```yaml
max-retry-credentials: 1
disable-cooling: true
```

The retry cap limited each CPA request to one credential, which prevented a
single request from walking the pool. However, generic 403 handling honored
`disable-cooling: true`, cleared the normal 30-minute cooldown, and left the
credential selectable for later requests. A credential-wide Google validation
failure was therefore treated like a model-local failure with no effective
cooldown.

CPA commit `c5dee5cf` adds a narrow classification for Antigravity status 403
responses containing `VALIDATION_REQUIRED` or `Verify your account to
continue`. That error now:

- keeps downstream status 403;
- is credential-scoped across models;
- enforces a 30-minute cooldown even when global cooling is disabled;
- leaves ordinary 403 behavior unchanged.

### Daemon kept validation-blocked accounts enabled

The daemon correctly kept ordinary unreadable quota results unchanged, because
transient 429/503 quota probes must not disable healthy credentials. The same
branch also kept validation-required 403 credentials enabled, even though the
inference endpoint returned the same account-level denial.

Daemon commit `a413a1f92` separates this explicit condition:

- an enabled account with validation-required 403 is disabled;
- the filename is recorded in `quota-disabled.json` as daemon-managed;
- a disabled account stays disabled while the validation error persists;
- once quota becomes readable, automatic recovery still requires both the
  weekly and 5-hour Gemini buckets to be at least 5%;
- ordinary quota read failures still keep the current status.

### A stale bind-mounted WebUI cleared all quota cards

Both CPA containers ran binary commit `1d57a59`, but
`/data/cli-proxy-api/static/management.html` was bind-mounted over the image's
newer management page.

The mounted file had SHA-256 `a6c754ce...` and still called
`clearQuotaCache()` after any auth-file mutation. Deleting or changing one auth
file therefore cleared every displayed quota. The image already contained a
newer file with SHA-256 `6eab7ded...`, which performs targeted invalidation.

On September 25 the mounted page was atomically replaced with the prebuilt file
from the running image. No server build and no CPA restart were performed. The
old page was retained as
`management.html.backup-20260925-1745`.

CPA commit `9597d0d1` further isolates in-flight requests by auth filename. A
mutation of account A now rejects only account A's stale quota response; account
B's displayed and in-flight quota remain valid. Full cache clearing is reserved
for connection/session changes and explicit full invalidation.

## Daemon timeline

The daemon began September 25 with 23 active accounts and progressively
disabled accounts only after confirmed weekly or 5-hour bucket exhaustion. At
14:30 server time it saw 100 auths with 2 active and enabled one healthy account.
By 16:05 it saw 99 auths with 0 active. After a manual batch enable, the 17:39
cycle saw 94 auths with 25 active and disabled four accounts whose 5-hour bucket
was confirmed at zero. The following cycle still kept seven active
validation-required accounts because the old daemon did not classify that
condition separately.

## Verification

The fixes were verified locally without sending model traffic:

```text
webui: 436 tests passed; ESLint passed; TypeScript and Vite production build passed
CPA: go test ./internal/runtime/executor ./sdk/cliproxy/auth passed
CPA: go build -o /tmp/cli-proxy-api-validation ./cmd/server passed
daemon: 26 unit tests passed; py_compile passed
```

The regression coverage locks in three contracts:

1. Targeted auth-file invalidation cannot cancel an unrelated in-flight quota
   request.
2. Antigravity validation-required 403 is credential-scoped and forces
   cooldown while ordinary 403 still respects the configured cooling override.
3. The daemon disables only the explicit validation-required quota error while
   preserving account state for ordinary unreadable quota responses.

## Deployment rule

VPS196 must not compile CPA. Build the multi-architecture image in GitHub
Actions, pull the resulting immutable digest, update blue and green one at a
time, and verify version/digest plus health before retiring the previous image.
The daemon is a standalone tested Python file and may be copied atomically with
its previous version retained for rollback.
