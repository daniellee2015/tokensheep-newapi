# Email verification intermittently rejected across instances

## Symptom and cause

On 2026-10-15, a user received a registration code, saw “Verification code is incorrect or has expired” on the first submission, and successfully registered on the second submission without changing the code.

The deployed revision, `249ea99`, stored verification codes in a process-local map. TokenSheep runs two application instances behind a load balancer. A submission handled by an instance other than the issuer could not find the code, although both instances already had access to the same Redis database. The same storage mechanism also served password-reset tokens.

## Correction

When Redis is enabled, store and read verification codes in Redis with the existing ten-minute lifetime. Keys include the verification purpose and a SHA-256 hash of the email address. Replacing or deleting a code is visible to every instance. A Redis failure must not silently fall back to process-local state; an unsuccessful write prevents sending an unusable verification email or password-reset link. Password-reset responses preserve the existing protection against email enumeration.

Standalone deployments without Redis retain their existing local behavior. Verification and deletion remain separate operations, preserving the callers' existing semantics.

## Validation

- A regression test using two independent Redis clients and clearing local state failed against the original implementation and passed after the fix.
- Regression coverage includes wrong codes, different email addresses and purposes, replacement, expiration, deletion, Redis failure, and standalone operation.
- `go test ./common ./controller` passed.

## Rollout

Deploy both application instances from this revision, checking health after each replacement. Codes issued before the rollout were only stored in the old processes and may need to be requested again. SMTP and Google OAuth settings are independent of this correction and remain in the existing production configuration.
