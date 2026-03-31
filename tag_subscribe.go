package gologix

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

func snapshotValue(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		cp := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		reflect.Copy(cp, rv)
		return cp.Interface()
	case reflect.Array:
		cp := reflect.New(rv.Type()).Elem()
		cp.Set(rv)
		return cp.Interface()
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		cp := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			cp.SetMapIndex(iter.Key(), iter.Value())
		}
		return cp.Interface()
	default:
		return v
	}
}

// TagSubscriptionConfig defines behavior for tag-based logical subscriptions.
//
// This is not Class 1 implicit I/O. It is a periodic explicit read that emits
// value updates when the tag value changes.
type TagSubscriptionConfig struct {
	Tag string

	// PollInterval controls how frequently the tag is read.
	// If <= 0, a default of 200ms is used.
	PollInterval time.Duration

	// EmitInitial controls whether to emit the first successful read immediately.
	// Default is false.
	EmitInitial bool

	// ValueBuffer controls the size of Values channel.
	// Default is 32.
	ValueBuffer int

	// ErrorBuffer controls the size of Errors channel.
	// Default is 8.
	ErrorBuffer int
}

// TagSubscription streams decoded tag values and non-fatal read errors.
type TagSubscription[T any] struct {
	Values <-chan T
	Errors <-chan error

	values chan T
	errs   chan error
	stopCh chan struct{}
	doneCh chan struct{}
}

// TagUpdate represents a value update for one tag in a multi-tag subscription.
type TagUpdate struct {
	Tag       string
	Value     any
	Timestamp time.Time
}

// TagMultiSubscriptionConfig defines behavior for scalable multi-tag subscriptions.
//
// Tags map keys are tag names and values are typed zero-values (or pre-allocated
// slices) used by ReadMap to infer CIP decoding types.
type TagMultiSubscriptionConfig struct {
	Tags map[string]any

	// PollInterval controls loop cadence.
	// If <= 0, a default of 100ms is used.
	PollInterval time.Duration

	// BatchSize controls how many tags are read in one ReadMap request.
	// If <= 0, a default of 200 is used.
	BatchSize int

	// BatchesPerTick controls how many batches are scanned per poll interval.
	// If <= 0, a default of 1 is used.
	BatchesPerTick int

	// EmitInitial controls whether first successful read of each tag emits an update.
	EmitInitial bool

	// ValueBuffer controls the size of Values channel.
	// Default is 256.
	ValueBuffer int

	// ErrorBuffer controls the size of Errors channel.
	// Default is 32.
	ErrorBuffer int
}

// TagMultiSubscription streams per-tag updates and non-fatal read errors.
type TagMultiSubscription struct {
	Values <-chan TagUpdate
	Errors <-chan error

	values chan TagUpdate
	errs   chan error
	stopCh chan struct{}
	doneCh chan struct{}
}

// SubscribeTag creates a logical subscription for a normal Logix tag.
//
// Values are produced by periodic explicit reads and emitted on change.
func SubscribeTag[T any](client *Client, cfg TagSubscriptionConfig) (*TagSubscription[T], error) {
	if err := client.checkConnection(); err != nil {
		return nil, fmt.Errorf("could not start tag subscription: %w", err)
	}

	if strings.TrimSpace(cfg.Tag) == "" {
		return nil, fmt.Errorf("tag must not be empty")
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.ValueBuffer <= 0 {
		cfg.ValueBuffer = 32
	}
	if cfg.ErrorBuffer <= 0 {
		cfg.ErrorBuffer = 8
	}

	values := make(chan T, cfg.ValueBuffer)
	errs := make(chan error, cfg.ErrorBuffer)

	sub := &TagSubscription[T]{
		Values: values,
		Errors: errs,

		values: values,
		errs:   errs,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	go sub.run(client, cfg.Tag, cfg.PollInterval, cfg.EmitInitial)
	return sub, nil
}

// SubscribeTags creates a scalable logical subscription for many normal Logix tags.
//
// It uses batched ReadMap calls and emits per-tag updates only when values change.
func SubscribeTags(client *Client, cfg TagMultiSubscriptionConfig) (*TagMultiSubscription, error) {
	if err := client.checkConnection(); err != nil {
		return nil, fmt.Errorf("could not start tag subscription: %w", err)
	}

	if len(cfg.Tags) == 0 {
		return nil, fmt.Errorf("tags must not be empty")
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.BatchesPerTick <= 0 {
		cfg.BatchesPerTick = 1
	}
	if cfg.ValueBuffer <= 0 {
		cfg.ValueBuffer = 256
	}
	if cfg.ErrorBuffer <= 0 {
		cfg.ErrorBuffer = 32
	}

	keys := make([]string, 0, len(cfg.Tags))
	for k := range cfg.Tags {
		if strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("tag key must not be empty")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	batches := make([]map[string]any, 0, (len(keys)+cfg.BatchSize-1)/cfg.BatchSize)
	for i := 0; i < len(keys); i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := make(map[string]any, end-i)
		for _, k := range keys[i:end] {
			batch[k] = cfg.Tags[k]
		}
		batches = append(batches, batch)
	}

	values := make(chan TagUpdate, cfg.ValueBuffer)
	errs := make(chan error, cfg.ErrorBuffer)

	sub := &TagMultiSubscription{
		Values: values,
		Errors: errs,

		values: values,
		errs:   errs,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	go sub.run(client, batches, cfg.PollInterval, cfg.BatchesPerTick, cfg.EmitInitial)
	return sub, nil
}

// Stop stops the background poll loop and closes channels.
func (sub *TagSubscription[T]) Stop() error {
	select {
	case <-sub.stopCh:
		// already stopped
	default:
		close(sub.stopCh)
	}

	<-sub.doneCh
	return nil
}

// Stop stops the background multi-tag poll loop and closes channels.
func (sub *TagMultiSubscription) Stop() error {
	select {
	case <-sub.stopCh:
		// already stopped
	default:
		close(sub.stopCh)
	}

	<-sub.doneCh
	return nil
}

func (sub *TagSubscription[T]) run(client *Client, tag string, pollInterval time.Duration, emitInitial bool) {
	defer close(sub.values)
	defer close(sub.errs)
	defer close(sub.doneCh)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	hasPrev := false
	var prev T

	for {
		select {
		case <-sub.stopCh:
			return
		case <-ticker.C:
			var cur T
			if err := client.Read(tag, &cur); err != nil {
				select {
				case sub.errs <- err:
				default:
				}
				continue
			}

			if !hasPrev {
				hasPrev = true
				prev = cur
				if emitInitial {
					select {
					case sub.values <- cur:
					default:
					}
				}
				continue
			}

			if !reflect.DeepEqual(cur, prev) {
				prev = cur
				select {
				case sub.values <- cur:
				default:
				}
			}
		}
	}
}

func (sub *TagMultiSubscription) run(client *Client, batches []map[string]any, pollInterval time.Duration, batchesPerTick int, emitInitial bool) {
	defer close(sub.values)
	defer close(sub.errs)
	defer close(sub.doneCh)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	prev := make(map[string]any, 1024)
	seen := make(map[string]struct{}, 1024)
	batchIndex := 0

	for {
		select {
		case <-sub.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < batchesPerTick; i++ {
				if len(batches) == 0 {
					break
				}

				batch := batches[batchIndex]
				batchIndex = (batchIndex + 1) % len(batches)

				if err := client.ReadMap(batch); err != nil {
					select {
					case sub.errs <- err:
					default:
					}
					continue
				}

				now := time.Now()
				for tag, cur := range batch {
					_, hasSeen := seen[tag]
					if !hasSeen {
						snap := snapshotValue(cur)
						seen[tag] = struct{}{}
						prev[tag] = snap
						if emitInitial {
							update := TagUpdate{Tag: tag, Value: snap, Timestamp: now}
							select {
							case sub.values <- update:
							default:
							}
						}
						continue
					}

					if !reflect.DeepEqual(cur, prev[tag]) {
						snap := snapshotValue(cur)
						prev[tag] = snap
						update := TagUpdate{Tag: tag, Value: snap, Timestamp: now}
						select {
						case sub.values <- update:
						default:
						}
					}
				}
			}
		}
	}
}
