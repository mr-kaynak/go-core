---
name: core-platform
description: Build or extend products using mr-kaynak/go-core and core-ui. Use for startup backends, business modules, admin screens, SDK consumers, migration jobs, dependency upgrades, and debugging these shared packages. Also evaluate whether this platform fits a proposed product. Not a generic Go/React recipe or a claim that tenancy, SSO, billing, or worker orchestration already exist.
---

# Core Platform

Turn a product request into consumer-owned code that reuses the shared platform without forking it. This package contains enough architectural and operational context to begin in an empty workspace; resolve current source before relying on an API signature.

**Frontend is optional.** `go-core` works as a standalone backend/API with no core-ui checkout, Node/Bun, React or admin app. Keep an existing web/mobile frontend if there is one; it can call the HTTP API directly. Add the React-free SDK only when its browser transport/session contract fits, shared UI components only when needed, and the full admin only when requested. A backend task does not imply scaffolding, installing or testing a frontend.

## Establish the task and checkout

Identify whether the user wants a new product, a module/page, a separate service, an integration, a repair, or an upgrade. Infer routine choices from their repository and request. Ask only for missing decisions that change ownership, security or compatibility (for example organizations versus individual owners); keep independent work moving.

Canonical repositories:

- `https://github.com/mr-kaynak/go-core` — Go application facade, identity, RBAC, PostgreSQL, migrations, outbox, notification infrastructure.
- `https://github.com/mr-kaynak/core-ui` — Bun workspace with React-free `@mr-kaynak/core-sdk`, React components `@mr-kaynak/core-ui`, and `@mr-kaynak/core-admin`.

Search the current workspace for `go.mod`, `package.json`, `AGENTS.md` and dependency declarations. Read local instructions and record the actual refs/versions. If source is missing, clone the required canonical repository into a dependency directory outside the product repository (or an explicitly ignored directory) or obtain the pinned module; never infer a machine-specific path. Installation never requires a checkout of either repository. A consumer may have neither repository checked out; use its pinned Go module cache/package artifacts or fetch individual template files when sufficient, cloning source only when the concrete development task needs it. Installed skill references are self-contained and do not depend on sibling repo paths.

Before coding, select the smallest suitable surface:

| Request | Default starting point | Read |
| --- | --- | --- |
| Is this a good fit? SaaS, startup, internal tool | Product boundary and operational requirements | [Architecture and scenarios](references/architecture.md) |
| Backend/API only, login and owned data | Copy `go-core/examples/startup`; frontend is optional | [Start a product](references/start-product.md) |
| Add a domain or endpoint | Consumer `app.Module` + optional migration source | [Backend contract](references/backend.md) |
| Add admin screen/menu/form | Consumer `AdminModule` with shared SDK/hooks | [Frontend contract](references/frontend.md) |
| Non-React client | SDK only; supply explicit session/storage strategy | [Frontend contract](references/frontend.md) |
| Add tenant, worker, service, billing provider | Explicit architecture extension; these are not prebuilt | [Architecture and scenarios](references/architecture.md), then backend |
| Migration, deploy, dependency upgrade | Same source inventory for serving and DDL job | [Operations and verification](references/operations.md) |
| Bug or review | Reproduce through the actual public path and preserve guards | Relevant contract, then operations |

Load the applicable references, not every file by default. The user's explicit technology or deployment choices override these defaults; explain a mismatch rather than replacing their stack silently.

## Non-negotiable platform boundaries

- Product logic belongs in its consumer repo. Core `internal/` is not a public toolbox. Supported Go surfaces are `app`, `identity`, `coremigrations`; Fiber v3 and GORM are intentionally exposed today.
- Start with a modular monolith unless independent scaling, ownership or isolation warrants another executable/database. `WithModules` does not create microservices or SSO.
- Core RBAC grants operations; consumer queries enforce owner/tenant access. A client-supplied owner or tenant header is never proof of membership.
- `Register` wires routes/dependencies only: no connections or goroutines. The public facade publishes events; it does not provide a subscription, notification-service/mailer, or worker lifecycle API.
- Migration source names are permanent DB identities. SQL lives at the root of an embedded FS. Never change applied SQL, rename deployed sources, bypass admission checks or AutoMigrate business tables during registration.
- Business write and event use one transaction and `app.ContextWithTx`; propagate dispatch errors. Only the outbox write is atomic. Local callbacks run before commit; external consumers need idempotency.
- UI routes and menu items each declare their own permission. Backend authorization is authoritative. A configured admin owns its router/providers; one live admin per page is supported.
- Dependencies are pinned to verified refs/artifacts. Local source replacement and tarball overrides prove different things from a registry install. Never claim release availability or a safe upgrade solely because compilation passed.

## Working agreement for a concrete task

Write a compact implementation contract in the product's docs when introducing a domain or architectural change: owner/tenant boundary, entities, API request/response/status shapes, permission names and objects, migration source, event payload/version, UI routes, failure/retry behavior and deployment source inventory. Scale this to the task; a one-endpoint repair does not need a design ceremony.

Implement through public surfaces. Use checked-in startup examples as executable templates, not prose snippets copied without imports or configuration. Extend shared code only when multiple consumers need the capability or the facade has a demonstrated gap.

Validate behavior appropriate to risk: compile/typecheck, real authorization and cross-owner denial, SQL migration and persistence, transaction failure, browser deep links and error states, and external consumer builds. [Operations](references/operations.md) defines commands and proof boundaries. New auth/permission/transaction regressions need meaningful tests; low-impact wording edits do not need invented tests.

Hand off changed consumer/core surfaces, pinned versions, commands/results, live versus mocked checks, migration/deploy order and remaining limitations. Do not expose secrets. Existing user authorization for push/deploy persists; the skill does not introduce an extra approval gate. Never publish a release or mutate production merely because a task mentions this platform.

## Example requests this skill can complete

- “Start a booking SaaS with an admin, users owning bookings, and email notifications.” Plan a consumer-owned notification integration or a tested facade extension (the public module context has no mailer), determine whether ownership is individual or organizational, scaffold a pinned consumer, add booking SQL/routes/UI, verify denied access and persistence.
- “Build a backend-only booking API; my mobile app already exists.” Use go-core, add booking SQL/routes/permissions and a migration job, document the HTTP contract, and verify it through HTTP tests. Do not install core-ui, Bun or React; native clients do not need the browser SDK.
- “Add invoices to this app.” Inspect existing identity and tenancy, add consumer billing schema/permissions/routes, use an explicit currency/amount and idempotency contract, register migrations, and add an admin module only if this app uses or requests the admin.
- “Move billing to its own service.” Define data and identity ownership, deployment and message delivery contracts first; do not share core tables or import private worker packages.
- “Upgrade our existing product to this core commit.” Inventory sources and previous refs, restore a disposable DB, migrate, run old-client and rollback checks, report what was actually proven.
- “Fix permission assignment showing an error after it succeeds.” Reproduce the HTTP status/content type and SDK parser together; protect error handling and old-backend compatibility.
