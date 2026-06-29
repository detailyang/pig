package triggers

import (
	"context"
	"fmt"
	"time"
)

type ScheduledCronNotificationHook struct {
	registry  *ScheduledCronRegistry
	adapter   *ScheduledCronAdapter
	status    NotificationHookStatus
	tickEvery time.Duration
}

type CronNotificationHook = ScheduledCronNotificationHook

func NewScheduledCronNotificationHook(registry *ScheduledCronRegistry) *ScheduledCronNotificationHook {
	if registry == nil {
		registry = NewScheduledCronRegistry()
	}
	return &ScheduledCronNotificationHook{registry: registry, adapter: NewScheduledCronAdapter(registry), status: PendingNotificationHookStatus(), tickEvery: time.Second}
}

func NewCronNotificationHook(registry *CronRegistry) *CronNotificationHook {
	return NewScheduledCronNotificationHook(registry)
}

func (hook *ScheduledCronNotificationHook) Label() string {
	return "cron"
}

func (hook *ScheduledCronNotificationHook) Run(ctx context.Context, sink TriggerSink) error {
	if hook == nil {
		return nil
	}
	hook.status.State = HookState{Kind: HookStateConnected}
	hook.status.LastError = nil
	ticker := time.NewTicker(hook.tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			for _, trigger := range hook.adapter.Poll(now.UTC()) {
				if err := sendCronTrigger(ctx, sink, trigger); err != nil {
					if hookErr, ok := err.(HookError); ok && hookErr.Kind == HookErrorSinkClosed {
						hook.markSinkClosed()
					}
					return err
				}
				eventAt := now.UTC()
				hook.status.LastEventAt = &eventAt
			}
		}
	}
}

func (hook *ScheduledCronNotificationHook) markSinkClosed() {
	message := "sink closed"
	hook.status.State = HookState{Kind: HookStateDisconnected, Reason: message}
	hook.status.LastError = &message
}

func sendCronTrigger(ctx context.Context, sink TriggerSink, trigger Trigger) (err error) {
	defer func() {
		if recover() != nil {
			err = HookError{Kind: HookErrorSinkClosed}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case sink <- trigger:
		return nil
	}
}

func (hook *ScheduledCronNotificationHook) Status() NotificationHookStatus {
	if hook == nil || hook.registry == nil {
		return PendingNotificationHookStatus()
	}
	status := hook.status
	jobs := hook.registry.List()
	var queued uint64
	var enabled int
	for _, job := range jobs {
		if job.RunningTraceID != "" {
			queued++
		}
		if job.Enabled {
			enabled++
		}
	}
	status.QueuedCount = queued
	if len(jobs) == 0 {
		status.SubscriptionLabels = []string{"local crontab: 0 jobs"}
	} else {
		status.SubscriptionLabels = []string{fmt.Sprintf("local crontab: %d job(s), %d enabled", len(jobs), enabled)}
	}
	return status
}
