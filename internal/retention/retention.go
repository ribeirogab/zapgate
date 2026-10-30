package retention

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	Interval         = 24 * time.Hour
	JournalRetention = 7 * 24 * time.Hour
	batchPause       = 100 * time.Millisecond
)

type Deps struct {
	Store         *store.Store
	Files         *media.Files
	Clock         clock.Clock
	Log           *slog.Logger
	DefaultMonths int
}

type Pass struct {
	Running    bool
	StartedAt  time.Time
	FinishedAt time.Time
	Messages   int
	Files      int
	Events     int
	Deliveries int
}

type Worker struct {
	deps Deps

	mu   sync.Mutex
	pass Pass
}

func New(deps Deps) *Worker {
	return &Worker{deps: deps}
}

func Months(account store.Account, defaultMonths int) int {
	if account.RetentionMonths > 0 {
		return account.RetentionMonths
	}
	return defaultMonths
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		started := w.runPass(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-w.deps.Clock.After(started.Add(Interval).Sub(w.deps.Clock.Now())):
		}
	}
}

func (w *Worker) LastPass() Pass {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pass
}

func (w *Worker) runPass(ctx context.Context) time.Time {
	started := w.begin()
	accounts, err := w.deps.Store.ListAccounts(ctx)
	w.report(ctx, "messages", err)
	for _, account := range accounts {
		if ctx.Err() != nil {
			break
		}
		w.report(ctx, "messages", w.expire(ctx, account, started), "account", account.Name)
	}
	w.report(ctx, "events", w.expireEvents(ctx, started))
	w.report(ctx, "deliveries", w.expireDeliveries(ctx, started))
	pass := w.finish()
	w.deps.Log.Info("retention pass finished", "deleted_messages", pass.Messages, "deleted_files", pass.Files,
		"deleted_events", pass.Events, "deleted_deliveries", pass.Deliveries)
	return started
}

func (w *Worker) expire(ctx context.Context, account store.Account, now time.Time) error {
	before := now.AddDate(0, -Months(account, w.deps.DefaultMonths), 0)
	return inBatches(ctx, func() (int, error) {
		batch, err := w.deps.Store.DeleteMessagesBefore(ctx, account.ID, before, w.removeFiles)
		if err != nil {
			return 0, err
		}
		w.count(func(p *Pass) {
			p.Messages += batch.Messages
			p.Files += batch.Files
		})
		return batch.Messages, nil
	})
}

func (w *Worker) expireEvents(ctx context.Context, now time.Time) error {
	before := now.Add(-JournalRetention)
	return inBatches(ctx, func() (int, error) {
		deleted, err := w.deps.Store.DeleteEventsBefore(ctx, before)
		w.count(func(p *Pass) { p.Events += deleted })
		return deleted, err
	})
}

func (w *Worker) expireDeliveries(ctx context.Context, now time.Time) error {
	before := now.Add(-JournalRetention)
	return inBatches(ctx, func() (int, error) {
		deleted, err := w.deps.Store.DeleteDeliveriesSettledBefore(ctx, before)
		w.count(func(p *Pass) { p.Deliveries += deleted })
		return deleted, err
	})
}

func inBatches(ctx context.Context, deleteBatch func() (int, error)) error {
	for {
		deleted, err := deleteBatch()
		if err != nil || deleted < store.RetentionBatch {
			return err
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}

func (w *Worker) removeFiles(paths []string) error {
	var errs []error
	for _, path := range paths {
		errs = append(errs, w.deps.Files.Remove(path))
	}
	return errors.Join(errs...)
}

func pause(ctx context.Context) error {
	timer := time.NewTimer(batchPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w *Worker) begin() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pass = Pass{Running: true, StartedAt: w.deps.Clock.Now()}
	return w.pass.StartedAt
}

func (w *Worker) count(add func(pass *Pass)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	add(&w.pass)
}

func (w *Worker) finish() Pass {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pass.Running = false
	w.pass.FinishedAt = w.deps.Clock.Now()
	return w.pass
}

func (w *Worker) report(ctx context.Context, rows string, err error, attrs ...any) {
	if err != nil && ctx.Err() == nil {
		w.deps.Log.Error("the retention pass cannot delete old "+rows, append(attrs, "error", err)...)
	}
}
