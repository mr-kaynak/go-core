# ADR-0004: Fail-Closed JWT Token Blacklist on Redis

- **Status:** Accepted
- **Date:** 2026-07-12

## Context

go-core authenticates with stateless HS256 JWTs. Statelessness is the point —
no session lookup on the hot path — but it means a signed token is valid until
its `exp` claim, full stop. Logout, password change, password reset, role
change, and admin-driven account deactivation all *need* revocation, and a
stateless token cannot be revoked without reintroducing server-side state
somewhere.

The threat model makes the failure mode concrete: if revocation silently stops
working, an attacker holding a stolen access token keeps a working credential
even after the victim changes their password, and a deactivated employee's
token keeps authenticating until it expires. Worse, the outage is invisible —
every request still succeeds, so nothing alerts. A revocation mechanism that
degrades to "everything is valid" is indistinguishable from no revocation at
all, precisely when it matters most.

So the design question is twofold: where does the revocation state live, and
what happens when that store is unreachable?

## Decision

Revocation state lives in Redis, and the check **fails closed**: once the
blacklist is wired, a Redis error means the token is treated as revoked.

Mechanics, as implemented in `internal/infrastructure/cache/token_blacklist.go`
and `internal/modules/identity/service/token_service.go`:

- **Hashes, not tokens.** Keys are `blacklist:{sha256(token)}` (`hashToken` →
  `crypto.HashSHA256Hex`), so a Redis compromise does not yield replayable
  credentials.
- **Self-cleaning TTL.** Every blacklist write uses the configured access-token
  lifetime (`cfg.JWT.Expiry`) as the Redis TTL — an upper bound on any
  outstanding token's remaining validity. Entries evaporate exactly when the
  tokens they block would have expired anyway; the blacklist never grows
  unbounded. (Writers: `Logout`, `ChangePassword`, password reset in
  `internal/modules/identity/service/auth_service.go`; role/status changes in
  `user_service.go` and `admin_service.go`.)
- **User-level revocation.** `blacklist:user:{userID}` blocks *all* of a user's
  tokens at once — the "revoke everything on password/role change" primitive.
  `ClearUserBlacklist` lifts it after a successful refresh so newly issued
  tokens are accepted (`token_blacklist.go`, `auth_service.go:328`).
- **Fail-closed return.** `IsBlacklisted` and `IsUserBlacklisted` return
  `(true, err)` on any Redis error. `TokenService.ValidateToken` surfaces this
  as `503 Service Unavailable` ("Token validation temporarily unavailable")
  rather than `401`, so clients and operators can tell an infra outage from a
  genuine revocation. The check runs under a 2-second timeout.
- **Circuit breaker, also fail-closed.** The Redis client wraps every call in
  a consecutive-failure circuit breaker (`internal/infrastructure/cache/
  redis.go`). When the breaker is open, `Exists` returns `ErrCircuitOpen`,
  which flows through the same fail-closed path — the breaker exists to stop
  hammering a dead Redis, not to soften the security posture.
- **Deliberate nuance — never-configured vs. configured-but-down.** Both entry
  points create the Redis client gracefully: if the startup ping fails,
  `redisClient` stays nil, `Services.SetBlacklist(nil)` is a no-op
  (`internal/modules/identity/wire.go`), and validation skips the check
  entirely (`if s.blacklist != nil`). `cmd/grpc/main.go` logs exactly this:
  "token blacklist disabled — revoked tokens stay valid until JWT expiry".
  Fail-closed applies only once the blacklist is wired — Redis reachable at
  startup, unreachable later. This keeps the skeleton runnable without Redis
  (dev, demos) while guaranteeing that a production deployment that *has*
  revocation never silently loses it mid-flight.

Both servers wire it: `internal/infrastructure/server/server.go:374` (HTTP,
via `cmd/api`) and `cmd/grpc/main.go:127` call
`identitySvcs.SetBlacklist(redisClient)`. The behavior is pinned by tests in
`internal/infrastructure/cache/token_blacklist_test.go` —
`TestTokenBlacklistFailClosedOnRedisDownForToken`,
`TestTokenBlacklistFailClosedWhenCircuitOpen`, and
`TestTokenBlacklistUserAddQueryAndGracefulDegradation` — and recorded as an
invariant in `CLAUDE.md` ("Token blacklist is fail-closed").

## Alternatives Considered

**Fail-open (availability over security).** Return `false` on Redis error and
let traffic flow. Rejected: it converts every Redis outage into a silent
revocation bypass. An attacker who can degrade Redis (or just wait for an
incident) gets stolen and deactivated tokens re-validated. Security controls
that disappear under load fail exactly when under attack.

**Short-lived access tokens, no blacklist.** Shrink `JWT_EXPIRY` to minutes
and let expiry do the revoking. Rejected as the *only* mechanism: it still
leaves a revocation-latency window equal to the token lifetime — unacceptable
for password-reset-after-compromise — and pushing lifetime low enough to close
that window turns every request path into a refresh storm. We keep lifetimes
short *and* blacklist; the TTL trick makes the blacklist cheap because of it.

**DB-backed blacklist (PostgreSQL).** Durable and already a hard dependency.
Rejected for the hot path: this check runs on every authenticated request in
both servers, and a table lookup plus expired-row cleanup job is strictly
worse than an O(1) Redis `EXISTS` with native TTL eviction.

**Opaque session tokens.** Store sessions server-side and hand out random
IDs — revocation becomes a row delete. Rejected because it gives up JWT
statelessness entirely: every request needs a store lookup to establish
identity at all, and the gRPC server would need the same session store. The
blacklist keeps the common case (valid token) verifiable from the signature,
with Redis consulted only as a deny-list.

## Consequences

Positive:

- Revocation is real: logout, password change/reset, and admin deactivation
  take effect within one round-trip, not one token lifetime.
- The blacklist is O(1), self-cleaning, and stores no secrets.
- Outages are loud (`503`s, breaker state-change logs) instead of silent
  security degradation.

Negative — and these are accepted costs, not oversights:

- **Redis becomes a hard availability dependency for all authenticated
  traffic once configured.** A Redis outage is a full auth outage for both
  HTTP and gRPC. Teams stamping this skeleton must either monitor Redis with
  the same severity as PostgreSQL or consciously run without it (degraded
  mode) and accept expiry-bounded revocation.
- The startup-ping nuance means a deploy racing a Redis blip can come up in
  degraded mode without the blacklist; the warning log is the only signal.

Mitigations: the circuit breaker bounds latency blowup during outages (fast
`ErrCircuitOpen` failures instead of pile-ups of 2-second timeouts); Redis HA
(Sentinel/managed Redis) addresses the availability risk directly; and the
503-vs-401 split plus breaker logs make the failure mode observable and
alertable.
