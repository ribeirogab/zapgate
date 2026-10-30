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
	Random func() float64
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
		next := d.dispatch(ctx)
		select {
		case <-ctx.Done():
			d.wg.Wait()
			return nil
		case <-d.wake:
		case <-d.deps.Clock.At(next):
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) time.Time {
	err := errors.Join(d.collect(ctx), d.sendDue(ctx))
	if err != nil {
		d.report(ctx, err)
		return d.nextPoll()
	}
	return d.nextWake(ctx)
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

func (d *Dispatcher) nextWake(ctx context.Context) time.Time {
	poll := d.nextPoll()
	if len(d.slots) == cap(d.slots) {
		return poll
	}
	due, ok, err := d.deps.Store.NextDeliveryDue(ctx)
	d.report(ctx, err)
	if err != nil || !ok || poll.Before(due) {
		return poll
	}
	return due
}

func (d *Dispatcher) nextPoll() time.Time {
	return d.deps.Clock.Now().Add(pollInterval)
}

func (d *Dispatcher) report(ctx context.Context, err error) {
	if err != nil && ctx.Err() == nil && !errors.Is(err, store.ErrNotFound) {
		d.deps.Log.Error("cannot dispatch the webhook deliveries", "error", err)
	}
}
