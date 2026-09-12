# Architecture and scenario selection

## Good fits

Backend and frontend adoption are independent. A go-core API can serve an existing website, native mobile app or another service without any core-ui package. The React-free SDK is optional and expects browser APIs/storage; non-browser clients should implement the documented HTTP/session contract for their runtime. Components can be used without the admin shell. Install/build only the selected layers.

| Product situation | Why use it | Product work still required |
| --- | --- | --- |
| B2B tool, internal operations system | Login, roles, permissions, administration, audit, shared UI | Domain tables/actions, data ownership, organizational rules |
| Startup MVP with durable workflows | Module registration and embedded SQL avoid a core fork | Product UX, service rules, validation, indexing and pagination |
| Several products maintained by one team | Versioned dependency boundaries centralize common fixes | Independent config, data, secrets, release compatibility |
| Existing Go product needs admin/identity infrastructure | Public facade and installable UI provide integration points | An explicit migration/integration plan; don't silently replace its auth |
| Custom frontend or non-React client | SDK can be used without the admin or React | Session storage and runtime capabilities, actual backend DTOs |

A static marketing site, CLI with no identity/database needs, offline-only app, or service with a mandated incompatible runtime may need none of this. Importing the full core solely for one small utility is a poor fit. For an existing mature identity/tenant architecture, evaluate interoperability before proposing replacement.

## Ownership model

Consumer executable → `app.New` + product modules → Fiber HTTP routes → shared authentication/RBAC + product owner-filtered queries → PostgreSQL. Product transaction → outbox row → broker/delivery infrastructure. Admin consumer → `createAdminApp` + product modules → SDK → HTTP API.

Core owns identity, roles, permissions, session primitives, migration execution and common infrastructure. Consumers own product data, public domain contracts and business rules. UI package owns shared presentation; admin package owns shell/built-ins; SDK owns transport and session lifecycle. An app is a composition root, not a fork of the package sources.

## Decisions requiring staff-level attention

### Individual ownership versus organizations

The startup example is strictly user-owned data, including for system administrators. For organizations, specify tenant identity, membership/roles, invitation lifecycle, cross-tenant admin policy and background-job context. Resolve tenant membership server-side. Add tenant predicates to every query, count, update, delete, export and file lookup. Choose composite uniqueness/indexes and foreign keys that preserve the tenant boundary. Test a valid user crossing to another tenant. A header, UUID or UI selector is only input.

### Modular monolith versus separate service

Default: one executable and database with domain modules. Split when independently deployable ownership, scaling or failure isolation justifies distributed contracts. Define authoritative identities, token issuer/audience verification, data ownership, timeouts, retries and idempotency. Two apps with distinct identity databases do not share login simply because both import go-core. Do not build SSO by sharing JWT secrets without a designed issuer/audience and revocation model.

The built-in gRPC entry point is not a public consumer server factory. Public `Module.Register` cannot own a worker lifecycle. Use an explicit consumer-owned executable for jobs/subscribers or extend the facade with a tested lifecycle contract. Do not import private core messaging/bootstrap code.

### Events and external side effects

Use immutable event type/payload versions and aggregate IDs. Outbox atomicity covers business data plus durable event, not email/payment delivery exactly once. A subscriber needs a durable idempotency key/receipt, bounded retries and poison-message policy. Never charge a card inside a pre-commit local event callback. Provider webhooks need signature verification and event deduplication before mutating domain state.

### Shared code versus product extension

Keep pricing policies, invoicing, booking rules, product schemas and branded screens in consumers. Move a capability into core only with a concrete reused contract, lifecycle, compatibility plan and an external-consumer test. Core configuration aliases expose more than an ideal stable API; pin versions and avoid binding consumers to undocumented fields.

### Deployment and scaling

Do not equate presence of Redis/RabbitMQ with validated multi-instance behavior. Review permission mutation/resynchronization and revocation guarantees before serving multiple replicas; a writer lock alone does not refresh another process's enforcer. Preserve fail-closed Redis behavior. Plan separate DDL privileges, backups/restore, readiness and the migration-to-serving transition. See operations for proof requirements.

## Organization migration sequence

Membership tables reference core users through `user_id` and consumer organizations through `organization_id`, with a unique pair. Define whether deleting a user removes memberships and whether owned business records are retained, reassigned or deleted; do not default to a cascade that loses audit data. Core example projects retain a restrictive user FK on hard deletion; user deletion needs an explicit product policy.

For an existing user-owned product use expand/backfill/contract: add organization/membership tables and nullable ownership columns, backfill idempotently with verified mappings, deploy reads/writes that preserve both old and new invariants, validate completeness, then add required constraints/remove old fields in a later compatible release. Migrations commit separately, so every intermediate state needs a serving plan.

Permission objects use Casbin keyMatch2, not arbitrary glob semantics. Prefer parameterized paths such as `/api/v1/orgs/:orgID/bookings` plus item paths; do not assume `/orgs/*/bookings` expresses a bounded middle wildcard. Test actual nested paths against the matcher, and still verify organization membership in the handler. RBAC matching does not enforce row tenancy.
