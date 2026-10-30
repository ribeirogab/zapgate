package webhooks

import (
	"context"
	"time"
)

const (
	maxQuiet   = 60 * time.Second
	maxMaxWait = 10 * time.Minute
)

type deliveryTiming struct {
	quiet   time.Duration
	maxWait time.Duration
}

func (o Options) applyTiming(_ context.Context, c *config) error {
	current := deliveryTiming{quiet: c.webhook.Quiet, maxWait: c.webhook.MaxWait}
	next, err := current.with(o.Quiet, o.MaxWait)
	if err != nil {
		return err
	}
	c.webhook.Quiet, c.webhook.MaxWait = next.quiet, next.maxWait
	return nil
}

func (t deliveryTiming) with(quiet, maxWait string) (deliveryTiming, error) {
	var (
		next deliveryTiming
		err  error
	)
	if next.quiet, err = durationOr(quiet, t.quiet, "the quiet window", DefaultQuiet); err != nil {
		return deliveryTiming{}, err
	}
	if next.maxWait, err = durationOr(maxWait, t.maxWait, "the max wait", DefaultMaxWait); err != nil {
		return deliveryTiming{}, err
	}
	return next, next.validate()
}

func durationOr(raw string, current time.Duration, label string, example time.Duration) (time.Duration, error) {
	if raw == "" {
		return current, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, invalid("%s %q is not a duration: use a value such as %s", label, raw, example)
	}
	return d, nil
}

func (t deliveryTiming) validate() error {
	if t.quiet < 0 || t.quiet > maxQuiet {
		return invalid("the quiet window %s is not valid: use a duration from 0s to 60s", t.quiet)
	}
	if t.maxWait < t.quiet || t.maxWait > maxMaxWait {
		return invalid("the max wait %s is not valid: use a duration from the quiet window (%s) to 10m", t.maxWait, t.quiet)
	}
	return nil
}
