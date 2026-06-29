package triggers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

const DefaultDynamicTriggerPollInterval = 10 * time.Minute

const DEFAULT_DYNAMIC_TRIGGER_POLL_INTERVAL_SECS = uint64(DefaultDynamicTriggerPollInterval / time.Second)

var configuredDynamicTriggerPollIntervalSecs atomic.Uint64

func init() {
	configuredDynamicTriggerPollIntervalSecs.Store(uint64(DefaultDynamicTriggerPollInterval / time.Second))
}

func SetDynamicTriggerPollIntervalSecs(seconds uint64) {
	if seconds < 1 {
		seconds = 1
	}
	configuredDynamicTriggerPollIntervalSecs.Store(seconds)
}

func DynamicTriggerPollIntervalSecs() uint64 {
	return configuredDynamicTriggerPollIntervalSecs.Load()
}

type DynamicCheckAdapter struct {
	registry *DynamicRegistry
	cwd      string
	interval time.Duration
	lastPoll time.Time
}

func NewDynamicCheckAdapter(registry *DynamicRegistry, cwd string, interval time.Duration) *DynamicCheckAdapter {
	if interval <= 0 {
		interval = DefaultDynamicTriggerPollInterval
	}
	return &DynamicCheckAdapter{registry: registry, cwd: cwd, interval: interval}
}

type DynamicTriggerCheckHook struct {
	adapter *DynamicCheckAdapter
	status  NotificationHookStatus
}

func NewDynamicTriggerCheckHook(registry *DynamicRegistry) *DynamicTriggerCheckHook {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "<unknown>"
	}
	return NewDynamicTriggerCheckHookWithInterval(registry, cwd, time.Duration(DynamicTriggerPollIntervalSecs())*time.Second)
}

func NewDynamicTriggerCheckHookWithInterval(registry *DynamicRegistry, cwd string, interval time.Duration) *DynamicTriggerCheckHook {
	status := PendingNotificationHookStatus()
	status.SubscriptionLabels = []string{"dynamic trigger periodic check"}
	return &DynamicTriggerCheckHook{adapter: NewDynamicCheckAdapter(registry, cwd, interval), status: status}
}

func (hook *DynamicTriggerCheckHook) WithInterval(registry *DynamicRegistry, cwd string, interval time.Duration) *DynamicTriggerCheckHook {
	return NewDynamicTriggerCheckHookWithInterval(registry, cwd, interval)
}

func (hook *DynamicTriggerCheckHook) Label() string {
	return "local:dynamic"
}

func (hook *DynamicTriggerCheckHook) Run(ctx context.Context, sink TriggerSink) error {
	if hook == nil {
		return nil
	}
	hook.status.State = HookState{Kind: HookStateConnected}
	if err := hook.pollAndSend(ctx, sink, time.Now().UTC()); err != nil {
		return err
	}
	ticker := time.NewTicker(hook.adapter.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := hook.pollAndSend(ctx, sink, now.UTC()); err != nil {
				return err
			}
		}
	}
}

func (hook *DynamicTriggerCheckHook) pollAndSend(ctx context.Context, sink TriggerSink, now time.Time) error {
	enabledCount := hook.adapter.enabledRuleCount()
	if enabledCount == 0 {
		return nil
	}
	trigger := hook.adapter.buildTrigger(now, enabledCount)
	if err := sendCronTrigger(ctx, sink, trigger); err != nil {
		if hookErr, ok := err.(HookError); ok && hookErr.Kind == HookErrorSinkClosed {
			message := "sink closed"
			hook.status.State = HookState{Kind: HookStateDisconnected, Reason: message}
		}
		return err
	}
	eventAt := now.UTC()
	hook.status.LastEventAt = &eventAt
	hook.status.LastError = nil
	return nil
}

func (hook *DynamicTriggerCheckHook) Status() NotificationHookStatus {
	if hook == nil {
		return PendingNotificationHookStatus()
	}
	return hook.status
}

func (adapter *DynamicCheckAdapter) Poll(now time.Time) []Trigger {
	if adapter.lastPoll.IsZero() {
		adapter.lastPoll = now
		return nil
	}
	if now.Before(adapter.lastPoll.Add(adapter.interval)) {
		return nil
	}
	adapter.lastPoll = now
	enabledCount := adapter.enabledRuleCount()
	if enabledCount == 0 {
		return nil
	}
	return []Trigger{adapter.buildTrigger(now, enabledCount)}
}

func (adapter *DynamicCheckAdapter) enabledRuleCount() int {
	if adapter.registry == nil {
		return 0
	}
	count := 0
	for _, rule := range adapter.registry.List() {
		if rule.Enabled {
			count++
		}
	}
	return count
}

func (adapter *DynamicCheckAdapter) buildTrigger(now time.Time, enabledCount int) Trigger {
	nowUTC := now.UTC()
	cwd := adapter.cwd
	if cwd == "" {
		if currentDir, err := os.Getwd(); err == nil {
			cwd = currentDir
		}
	}
	if cwd == "" {
		cwd = "<unknown>"
	}
	summary := fmt.Sprintf(
		"Periodic dynamic trigger check at local time %s / UTC %s with %d enabled rule(s); cwd: %s",
		now.Local().Format("2006-01-02 15:04:05 MST"),
		nowUTC.Format(time.RFC3339),
		enabledCount,
		cwd,
	)
	return Trigger{
		Source:            Source{Kind: SourceLocal, Subkind: "dynamic"},
		SourceKind:        SourceKindLocal,
		SourceLabel:       "local:dynamic",
		EventLabel:        "dynamic periodic check",
		PayloadVisibility: PayloadLocal,
		PayloadSummary:    &summary,
		IDempotencyKey:    fmt.Sprintf("local:dynamic:%d", nowUTC.UnixMilli()),
		ReplacementPolicy: ReplacementDrop,
		TraceID:           newDynamicTraceID(),
		Authority: Authority{
			PrincipalID:     "local:dynamic",
			PrincipalLabel:  "dynamic trigger checker",
			CredentialScope: ScopeUser,
		},
		ReceivedAt: now,
	}
}

func newDynamicTraceID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("dynamic-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
