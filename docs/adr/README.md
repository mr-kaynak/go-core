# Architecture Decision Records

This directory records the significant architectural decisions behind this skeleton, in
[Michael Nygard's ADR format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).
Each record captures the context at the time of the decision, the alternatives that were
considered, and the consequences we accepted — including the negative ones.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-modular-monolith.md) | Modular Monolith with DDD-Flavored Modules | Accepted |
| [0002](0002-transactional-outbox.md) | Transactional Outbox with PostgreSQL LISTEN/NOTIFY for Event Publishing | Accepted |
| [0003](0003-casbin-rbac.md) | Casbin for RBAC Authorization | Accepted |
| [0004](0004-fail-closed-token-blacklist.md) | Fail-Closed JWT Token Blacklist on Redis | Accepted |
| [0005](0005-explicit-dependency-injection.md) | Explicit Constructor-Based Dependency Injection | Accepted |

## Adding a new ADR

1. Copy the section structure of an existing record (Status/Date, Context, Decision,
   Alternatives Considered, Consequences).
2. Number it sequentially (`NNNN-short-slug.md`) and add a row to the table above.
3. An ADR is immutable once accepted: if a decision is revisited, write a new record that
   supersedes the old one and update both records' Status lines (`Superseded by ADR-NNNN`).
