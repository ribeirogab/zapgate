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
	Interval   = 24 * time.Hour
	batchPause = 100 * time.Millisecond
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
	w.report(ctx, err)
	for _, account := range accounts {
		if ctx.Err() != nil {
			break
		}
		w.report(ctx, w.expire(ctx, account, started), "account", account.Name)
	}
	pass := w.finish()
	w.deps.Log.Info("retention pass finished", "deleted_messages", pass.Messages, "deleted_files", pass.Files)
	return started
}

func (w *Worker) expire(ctx context.Context, account store.Account, now time.Time) error {
	before := now.AddDate(0, -Months(account, w.deps.DefaultMonths), 0)
	for {
		batch, err := w.deps.Store.DeleteMessagesBefore(ctx, account.ID, before, w.removeFiles)
		if err != nil {
			return err
		}
		w.count(batch)
		if batch.Messages < store.RetentionBatch {
			return nil
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

func (w *Worker) count(batch store.ExpiredBatch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pass.Messages += batch.Messages
	w.pass.Files += batch.Files
}

func (w *Worker) finish() Pass {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pass.Running = false
	w.pass.FinishedAt = w.deps.Clock.Now()
	return w.pass
}

func (w *Worker) report(ctx context.Context, err error, attrs ...any) {
	if err != nil && ctx.Err() == nil {
		w.deps.Log.Error("the retention pass cannot delete old messages", append(attrs, "error", err)...)
	}
}
