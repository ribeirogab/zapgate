package webhooks

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	pollInterval = time.Minute
	eventBatch   = 500
	attemptLease = 2 * TotalTimeout
)

type DispatcherDeps struct {
	Store  *store.Store
	Sender *Sender
	Clock  clock.Clock
	Audit  *audit.Logger
	Log    *slog.Logger
}

type Dispatcher struct {
	deps  DispatcherDeps
	wake  chan struct{}
	slots chan struct{}
	wg    sync.WaitGroup
}

func NewDispatcher(deps DispatcherDeps) *Dispatcher {
	return &Dispatcher{deps: deps, wake: make(chan struct{}, 1), slots: make(chan struct{}, MaxConcurrent)}
}

func (d *Dispatcher) Notice(evt any) {
	if _, ok := evt.(*events.Message); ok {
		d.Wake()
	}
}

func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) Run(ctx context.Context) error {
	for {
		wait := d.dispatch(ctx)
		select {
		case <-ctx.Done():
			d.wg.Wait()
			return nil
		case <-d.wake:
		case <-d.deps.Clock.After(wait):
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) time.Duration {
	err := errors.Join(d.collect(ctx), d.sendDue(ctx))
	if err != nil {
		d.report(ctx, err)
		return pollInterval
	}
	return d.untilNextDue(ctx)
}

func (d *Dispatcher) collect(ctx context.Context) error {
	webhooks, err := d.deps.Store.ListWebhooks(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, webhook := range webhooks {
		errs = append(errs, d.collectFor(ctx, webhook))
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) collectFor(ctx context.Context, w store.Webhook) error {
	match, err := d.matcherFor(ctx, w)
	if err != nil {
		return err
	}
	for {
		journal, err := d.deps.Store.EventsAfter(ctx, w.Cursor, eventBatch)
		if err != nil || len(journal) == 0 {
			return err
		}
		w.Cursor = journal[len(journal)-1].Seq
		queue := store.Queue{WebhookID: w.ID, Cursor: w.Cursor, Events: match.keep(journal), Place: place}
		if err := d.deps.Store.QueueEvents(ctx, queue); err != nil {
			return err
		}
	}
}

func (d *Dispatcher) sendDue(ctx context.Context) error {
	free := cap(d.slots) - len(d.slots)
	if free == 0 {
		return nil
	}
	due, err := d.deps.Store.DueDeliveries(ctx, d.deps.Clock.Now(), free)
	if err != nil {
		return err
	}
	for _, delivery := range due {
		if err := d.start(ctx, delivery); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) start(ctx context.Context, delivery store.Delivery) error {
	claimed, err := d.deps.Store.ClaimDelivery(ctx, delivery.ID, d.deps.Clock.Now().Add(attemptLease))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	d.slots <- struct{}{}
	d.wg.Go(func() {
		defer d.release()
		d.send(ctx, claimed)
	})
	return nil
}

func (d *Dispatcher) release() {
	<-d.slots
	d.Wake()
}

func (d *Dispatcher) untilNextDue(ctx context.Context) time.Duration {
	if len(d.slots) == cap(d.slots) {
		return pollInterval
	}
	due, ok, err := d.deps.Store.NextDeliveryDue(ctx)
	if err != nil || !ok {
		d.report(ctx, err)
		return pollInterval
	}
	return min(pollInterval, max(0, due.Sub(d.deps.Clock.Now())))
}

func (d *Dispatcher) report(ctx context.Context, err error) {
	if err != nil && ctx.Err() == nil && !errors.Is(err, store.ErrNotFound) {
		d.deps.Log.Error("cannot dispatch the webhook deliveries", "error", err)
	}
}
