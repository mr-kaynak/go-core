# ADR-0002: Transactional Outbox with PostgreSQL LISTEN/NOTIFY for Event Publishing

- **Status:** Accepted
- **Date:** 2026-07-12

## Context

Business operations frequently need to persist state and announce it — e.g. a user
registers (row committed) and a `user.registered` event must reach RabbitMQ so other
components (notifications, audit) react. This is the classic dual-write problem: a
PostgreSQL commit and an AMQP publish are two independent systems with no shared
transaction. Whatever order we pick, a crash between the two operations either loses
the event (commit succeeded, publish never happened) or emits a phantom event
(publish succeeded, transaction rolled back).

Additional constraints shaped the decision:

- **RabbitMQ is optional.** The skeleton must start and run when the broker is down
  (see "Graceful Degradation" in `CLAUDE.md`); events must survive the outage.
- **Multiple replicas.** Both `cmd/api` and `cmd/grpc` run the relay; the same event
  must not be double-claimed across pods.
- **Low latency without hot polling.** Events should reach the broker in tens of
  milliseconds, but a skeleton should not hammer the database with tight poll loops.

## Decision

Events are written to the `outbox_messages` table **inside the business transaction**
and relayed to RabbitMQ asynchronously by an in-process processor that is woken via
PostgreSQL LISTEN/NOTIFY, with periodic polling as a safety net.

1. **Outbox write in the same transaction.**
   `RabbitMQService.PublishMessage(ctx, tx, msg)`
   (`internal/infrastructure/messaging/rabbitmq/rabbitmq_service.go`) does not touch
   the broker at all — it only inserts an `OutboxMessage` row, via
   `OutboxRepository.CreateMessageTx(tx, msg)` when a transaction is supplied
   (`internal/infrastructure/messaging/repository/outbox_repository.go`). Domain
   events take the same path: `events.ContextWithTx(ctx, tx)` carries the open
   transaction through the context, and `EventDispatcher.Dispatch` extracts it with
   `txFromContext` before calling `PublishMessage`
   (`internal/infrastructure/messaging/events/event_dispatcher.go`). This is the
   `CLAUDE.md` invariant "Outbox rows must be written inside the business
   transaction": the event commits or rolls back atomically with the business write.

2. **LISTEN/NOTIFY wake-up.** Migration
   `platform/migrations/00003_outbox_listen_notify.sql` installs a trigger function
   that runs `pg_notify('outbox_new_message', NEW.id::text)` on INSERT of a
   `pending` row and on UPDATE transitions back to `pending` (DLQ reprocess). A
   dedicated pgx connection in
   `internal/infrastructure/messaging/listener/outbox_listener.go` LISTENs on that
   channel, coalesces notification bursts over a 50 ms window into a capacity-1
   signal channel, and reconnects with exponential backoff (max 30 s) if the
   connection drops. Both entry points wire `outboxListener.SignalCh()` into
   `NewRabbitMQService` (`cmd/api/main.go`, `cmd/grpc/main.go`).

3. **Polling fallback.** `processOutboxMessages` in `rabbitmq_service.go` selects on
   the listener signal *and* a 60-second fallback ticker, and runs one batch at
   startup to drain rows inserted before the listener connected. If NOTIFY is ever
   missed (listener reconnecting, signal coalesced away), the row is picked up
   within a minute rather than never.

4. **Multi-replica safety via `FOR UPDATE SKIP LOCKED`.**
   `OutboxRepository.ClaimMessagesForProcessing` claims pending and retryable rows
   in one transaction using `SELECT ... FOR UPDATE SKIP LOCKED` and batch-updates
   them to `processing` before releasing the lock
   (`internal/infrastructure/messaging/repository/outbox_repository.go`).
   Concurrent pods skip each other's locked rows instead of blocking or
   double-publishing.

5. **Retry and DLQ.** Message lifecycle is `pending → processing → sent` on success,
   or `→ failed` with exponential backoff (`IncrementRetry`: 2^n seconds, capped at
   300 s) up to `MaxRetries` (default 3), then `→ dlq`
   (`internal/infrastructure/messaging/domain/outbox.go`). Exhausted messages are
   copied to `outbox_dead_letters` by `MoveToDLQ`, and `ReprocessDLQMessage` resets
   the original row to `pending` — which re-fires the NOTIFY trigger. The relay
   itself publishes with publisher confirms enabled (`PublishDirectly` waits for the
   broker ack), so `sent` means the broker accepted the message.

6. **Graceful degradation.** If RabbitMQ is down, `processOutboxBatch` returns early
   (`isConnected` is false) and rows simply accumulate in `outbox_messages`; after
   the reconnect loop restores the connection, the fallback ticker (or next NOTIFY)
   drains the backlog. If the broker is down at startup, `cmd/api/main.go` logs a
   warning and runs without messaging entirely.

## Alternatives Considered

- **Direct publish from services.** Simplest, but re-creates the dual-write problem:
  publish-after-commit loses events on a crash in between; publish-before-commit
  emits phantom events for rolled-back transactions. Wrapping publish in the DB
  transaction does not help — AMQP is not a transactional resource. Rejected.

- **CDC (Debezium / logical replication).** Tails the WAL, so it also achieves
  atomicity without a second write path, and scales to high volume. But it adds a
  Kafka-Connect-class runtime, replication-slot management, and schema/snapshot
  operations — heavy operational weight for a project skeleton whose broker is
  itself optional. The outbox relay is ~700 lines of in-process Go with no extra
  infrastructure. Rejected for this codebase; a high-volume fork could swap the
  relay for Debezium's outbox event router without changing producers.

- **Plain polling only.** Correct and simple, but forces a latency/load tradeoff:
  a 60 s interval means up to 60 s event lag; a 100 ms interval means constant
  query pressure on an idle table. LISTEN/NOTIFY gives near-immediate dispatch on
  the write path that already exists (a trigger), and we keep the 60 s poll purely
  as a missed-notification backstop. Rejected as the sole mechanism.

## Consequences

Positive:

- **No lost or phantom events.** The event row commits atomically with the business
  write; the relay retries until the broker confirms.
- **Broker outages are absorbed.** Events queue in PostgreSQL and drain on recovery.
- **Horizontal scale is safe.** `SKIP LOCKED` claiming lets every replica run the
  relay without coordination.
- **Ordering and audit for free.** Claims are ordered by `priority DESC, created_at
  ASC` (backed by partial indexes from migration `00003`), and
  `outbox_processing_logs` records every attempt.

Negative:

- **At-least-once delivery.** A crash after the broker confirm but before the row is
  marked `sent` re-publishes the message; consumers must be idempotent (dedupe on
  `Message.ID` / `CorrelationID`).
- **Extra hop latency.** commit → NOTIFY → claim → publish adds tens of milliseconds
  versus a direct publish, and up to 60 s in the rare fallback-poll case.
- **Table growth needs housekeeping.** `runCleanupJobs` deletes `sent` rows older
  than `RabbitMQ.ProcessedMessageRetention` hourly; DLQ rows require manual review.
- **Events pre-serialize state.** The payload is what the producer wrote, not the
  current row — consumers reading much later may see stale data in the payload.
- **In-process handlers fire pre-commit.** `Dispatch` runs local handlers and
  channel subscribers immediately, before the caller's transaction commits — only
  the RabbitMQ leg gets the outbox guarantee (documented on `Dispatch` in
  `event_dispatcher.go`).
- **TTL pressure.** `PublishMessage` stamps a 300 s TTL; if the broker outage
  outlasts it, `CleanupExpiredMessages` moves still-pending rows to the DLQ, where
  they need manual reprocessing rather than automatic recovery.
