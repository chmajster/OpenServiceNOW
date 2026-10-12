package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type Store interface {
	Claim(context.Context, string, time.Duration) (Event, bool, error)
	Heartbeat(context.Context, uint64, string, time.Duration) (HeartbeatState, error)
	Complete(context.Context, uint64, string) error
	Fail(context.Context, uint64, string, error, time.Duration) error
	Distribute(context.Context, uint64, string, []string) error
	ClaimDelivery(context.Context, string, time.Duration) (Delivery, bool, error)
	HeartbeatDelivery(context.Context, uint64, string, time.Duration) (HeartbeatState, error)
	FinishDelivery(context.Context, uint64, string, error, time.Duration) error
}

type Handler func(context.Context, Event) error

type Dispatcher struct {
	store         Store
	logger        *slog.Logger
	owner         string
	concurrency   int
	pollInterval  time.Duration
	leaseDuration time.Duration
	retryDelay    time.Duration
	handlers      map[string]map[string]Handler
}

func NewDispatcher(
	store Store,
	logger *slog.Logger,
	owner string,
	concurrency int,
	pollInterval time.Duration,
	leaseDuration time.Duration,
	retryDelay time.Duration,
) (*Dispatcher, error) {
	if store == nil || logger == nil || owner == "" ||
		concurrency < 1 || concurrency > 64 ||
		pollInterval < 50*time.Millisecond ||
		leaseDuration < 5*time.Second || leaseDuration > time.Hour ||
		retryDelay < 0 || retryDelay > 24*time.Hour {
		return nil, ErrInvalid
	}
	return &Dispatcher{
		store:         store,
		logger:        logger,
		owner:         owner,
		concurrency:   concurrency,
		pollInterval:  pollInterval,
		leaseDuration: leaseDuration,
		retryDelay:    retryDelay,
		handlers:      make(map[string]map[string]Handler),
	}, nil
}

func (d *Dispatcher) Register(eventType string, handler Handler) error {
	return d.RegisterConsumer("activity.v1", eventType, handler)
}

func (d *Dispatcher) RegisterConsumer(consumerID, eventType string, handler Handler) error {
	if !consumerPattern.MatchString(consumerID) || eventType == "" || len(eventType) > 120 || handler == nil {
		return ErrInvalid
	}
	if d.handlers[eventType] == nil {
		d.handlers[eventType] = make(map[string]Handler)
	}
	if _, exists := d.handlers[eventType][consumerID]; exists {
		return fmt.Errorf("event consumer %q already registered for %q", consumerID, eventType)
	}
	d.handlers[eventType][consumerID] = handler
	return nil
}

func (d *Dispatcher) RunOnce(ctx context.Context) (bool, error) {
	return d.claimAndDispatch(ctx, d.owner+"/once")
}

func (d *Dispatcher) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, d.concurrency)
	for index := 0; index < d.concurrency; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			if err := d.loop(runCtx, fmt.Sprintf("%s/%d", d.owner, slot)); err != nil &&
				!errors.Is(err, context.Canceled) {
				select {
				case errCh <- err:
				case <-runCtx.Done():
				}
			}
		}(index + 1)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		cancel()
		<-done
		return nil
	case err := <-errCh:
		cancel()
		<-done
		return err
	case <-done:
		return nil
	}
}

func (d *Dispatcher) loop(ctx context.Context, owner string) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			processed, err := d.claimAndDispatch(ctx, owner)
			if err != nil {
				d.logger.Error("outbox dispatcher iteration failed", "owner", owner, "error", err)
				timer.Reset(d.pollInterval)
				continue
			}
			if processed {
				timer.Reset(0)
			} else {
				timer.Reset(d.pollInterval)
			}
		}
	}
}

func (d *Dispatcher) claimAndDispatch(ctx context.Context, owner string) (bool, error) {
	// Both stages make progress on every iteration: a busy stream of new events
	// must not starve delivery attempts, and a failing consumer must not block split.
	event, ok, err := d.store.Claim(ctx, owner, d.leaseDuration)
	if err != nil {
		return false, err
	}
	if ok {
		consumers := make([]string, 0, len(d.handlers[event.Type]))
		for consumer := range d.handlers[event.Type] {
			consumers = append(consumers, consumer)
		}
		sort.Strings(consumers)
		if len(consumers) == 0 {
			deliveryErr := fmt.Errorf("no handler registered for event type %q", event.Type)
			finalCtx, cancel := finalizationContext(ctx)
			defer cancel()
			if err := d.store.Fail(finalCtx, event.ID, owner, deliveryErr, d.retryDelay); err != nil {
				return true, errors.Join(deliveryErr, err)
			}
		} else if err := d.store.Distribute(ctx, event.ID, owner, consumers); err != nil {
			return true, err
		}
	}
	delivery, deliveryOK, err := d.store.ClaimDelivery(ctx, owner, d.leaseDuration)
	if err != nil {
		return ok, err
	}
	if !deliveryOK {
		return ok, nil
	}
	event = delivery.Event
	handler := d.handlers[event.Type][delivery.ConsumerID]
	if handler == nil {
		return true, d.store.FinishDelivery(ctx, delivery.ID, owner, ErrInvalid, d.retryDelay)
	}

	eventCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan bool, 1)
	go func() {
		interval := d.leaseDuration / 3
		if interval < time.Second {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-eventCtx.Done():
				heartbeatDone <- true
				return
			case <-ticker.C:
				state, err := d.store.HeartbeatDelivery(eventCtx, delivery.ID, owner, d.leaseDuration)
				if err != nil || !state.LeaseValid {
					if err != nil {
						d.logger.Error("outbox heartbeat failed", "event_id", event.ID, "error", err)
					}
					cancel()
					heartbeatDone <- false
					return
				}
			}
		}
	}()

	deliveryErr := handler(eventCtx, event)
	cancel()
	leaseValid := <-heartbeatDone
	if !leaseValid {
		d.logger.Warn("outbox lease lost", "event_id", event.ID, "event_type", event.Type)
		return true, nil
	}
	finalCtx, finalCancel := finalizationContext(ctx)
	defer finalCancel()
	if err := d.store.FinishDelivery(finalCtx, delivery.ID, owner, deliveryErr, d.retryDelay); err != nil &&
		!errors.Is(err, ErrLeaseLost) {
		return true, err
	}
	return true, nil
}

func finalizationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
}
