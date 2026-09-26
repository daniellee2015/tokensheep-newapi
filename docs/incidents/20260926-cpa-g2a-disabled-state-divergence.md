# CPA and g2a disabled-state divergence (2026-09-26)

## Symptom

CPA had 90 Antigravity accounts but only 15 enabled. g2a had 95 accounts and all
95 were routable. Several CPA-disabled accounts had fresh weekly and 5-hour Gemini
quota, yet CPA never reopened them.

## Root cause

`cpa-daemon-v4` only recovered filenames stored in
`/var/lib/cpa-daemon-v4/quota-disabled.json`. A disabled account without that old
daemon marker was classified as `manual off` forever, even when both quota buckets
were healthy. This left accounts closed by an older daemon, an operator incident
response, or state migration permanently outside the CPA pool.

g2a has separate account files and no synchronization with CPA. Its quota protection
was disabled, so all accounts remained routable there, including credentials that
later returned Google `VALIDATION_REQUIRED` at inference time. g2a quota snapshots
therefore must not be used alone to decide that an account is healthy.

## Fix

The daemon now treats current quota and credential health as the source of truth for
recovery. Any disabled account is eligible to reopen when all of these conditions hold:

- the quota request succeeds and does not return `VALIDATION_REQUIRED`;
- `gemini-weekly` is at least 5%;
- `gemini-5h` is at least 5%;
- the active-pool and per-cycle recovery caps have room.

Accounts with exhausted quota, unreadable or missing buckets, validation failures, or
dead refresh credentials stay closed. The state file is still maintained for audit and
cleanup, but it is no longer a prerequisite for safe quota-based recovery.

## Operational note

CPA and g2a remain independent pools. CPA quota decisions do not automatically mutate
g2a account files. Until g2a persists account-level validation and quota protection
reliably, CPA's direct quota probe is the authoritative recovery signal.
