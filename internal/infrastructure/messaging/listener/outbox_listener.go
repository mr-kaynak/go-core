package listener

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

const (
	listenChannel  = "outbox_new_message"
	coalesceWindow = 50 * time.Millisecond
	waitTimeout    = 30 * time.Second
	maxBackoff     = 30 * time.Second
)

// OutboxListener listens for PostgreSQL LISTEN/NOTIFY events on the outbox channel.
type OutboxListener struct {
	dsn      string
	signalCh chan struct{}
	logger   *logger.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewOutboxListener creates a new OutboxListener with the given DSN.
func NewOutboxListener(dsn string) *OutboxListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &OutboxListener{
		dsn:      dsn,
		signalCh: make(chan struct{}, 1),
		logger:   logger.Get().WithFields(logger.Fields{"component": "outbox-listener"}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SignalCh returns the read-only signal channel that fires when new outbox messages arrive.
func (l *OutboxListener) SignalCh() <-chan struct{} {
	return l.signalCh
}

// Start begins the listener loop in a background goroutine.
func (l *OutboxListener) Start() {
	l.wg.Add(1)
	go l.listenLoop()
}

// Close shuts down the listener and waits for the goroutine to finish.
func (l *OutboxListener) Close() {
	l.cancel()
	l.wg.Wait()
}

// listenLoop connects to PostgreSQL and runs the wait loop with automatic reconnection.
func (l *OutboxListener) listenLoop() {
	defer l.wg.Done()

	backoff := time.Second

	for {
		if l.ctx.Err() != nil {
			return
		}

		conn, err := pgx.Connect(l.ctx, l.dsn)
		if err != nil {
			l.logger.Error("Failed to connect for LISTEN", "error", err, "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-l.ctx.Done():
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// LISTEN on the outbox channel
		_, err = conn.Exec(l.ctx, "LISTEN "+listenChannel)
		if err != nil {
			l.logger.Error("Failed to execute LISTEN", "error", err)
			conn.Close(l.ctx)
			select {
			case <-time.After(backoff):
			case <-l.ctx.Done():
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		l.logger.Info("Outbox listener connected and listening")
		backoff = time.Second

		// Run the wait loop — returns when connection drops or context is cancelled
		l.waitLoop(conn)
		conn.Close(context.Background())

		if l.ctx.Err() != nil {
			return
		}

		l.logger.Warn("Outbox listener disconnected, reconnecting...", "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-l.ctx.Done():
			return
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// waitLoop waits for notifications and sends signals on the channel.
func (l *OutboxListener) waitLoop(conn *pgx.Conn) {
	for {
		if l.ctx.Err() != nil {
			return
		}

		waitCtx, waitCancel := context.WithTimeout(l.ctx, waitTimeout)
		_, err := conn.WaitForNotification(waitCtx)
		waitCancel()

		if err != nil {
			// Timeout is expected — just loop to check context
			if l.ctx.Err() != nil {
				return
			}
			if waitCtx.Err() == context.DeadlineExceeded {
				continue
			}
			// Connection error — break to reconnect
			l.logger.Error("WaitForNotification error", "error", err)
			return
		}

		// Coalesce burst notifications: wait a short window to absorb rapid-fire INSERTs
		timer := time.NewTimer(coalesceWindow)
		drain := true
		for drain {
			select {
			case <-timer.C:
				drain = false
			case <-l.ctx.Done():
				timer.Stop()
				return
			}
		}

		// Drain any additional notifications that arrived during the coalesce window
		for {
			drainCtx, drainCancel := context.WithTimeout(l.ctx, time.Millisecond)
			_, drainErr := conn.WaitForNotification(drainCtx)
			drainCancel()
			if drainErr != nil {
				break
			}
		}

		// Non-blocking send — cap(1) provides natural dedup
		select {
		case l.signalCh <- struct{}{}:
		default:
		}
	}
}
