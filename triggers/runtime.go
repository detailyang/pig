package triggers

import (
	"sync"
	"time"
)

type RuntimeConfig struct {
	DedupWindow   time.Duration
	CycleHopLimit uint32
}

type TriggerRuntimeConfig = RuntimeConfig

const (
	TriggerRuntimeDefaultDedupWindow   = 5 * time.Minute
	TriggerRuntimeDefaultCycleHopLimit = 5
	TriggerRuntimeMaxDedupWindow       = 24 * time.Hour
	DEFAULT_DEDUP_WINDOW               = TriggerRuntimeDefaultDedupWindow
	DEFAULT_CYCLE_HOP_LIMIT            = TriggerRuntimeDefaultCycleHopLimit
	MAX_DEDUP_WINDOW                   = TriggerRuntimeMaxDedupWindow
)

func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{DedupWindow: TriggerRuntimeDefaultDedupWindow, CycleHopLimit: TriggerRuntimeDefaultCycleHopLimit}
}

func DefaultTriggerRuntimeConfig() TriggerRuntimeConfig {
	return DefaultRuntimeConfig()
}

type OutcomeKind string

const (
	OutcomeAccept          OutcomeKind = "accept"
	OutcomeDeduped         OutcomeKind = "deduped"
	OutcomeCycleSuppressed OutcomeKind = "cycle_suppressed"

	EvaluationOutcomeAccept          = OutcomeAccept
	EvaluationOutcomeDeduped         = OutcomeDeduped
	EvaluationOutcomeCycleSuppressed = OutcomeCycleSuppressed
)

type EvaluationOutcome struct {
	Kind              OutcomeKind
	ReplacementPolicy ReplacementPolicy
	PreviousTraceID   string
	HopCount          uint32
}

type RuntimeSnapshot struct {
	DedupEntries         int
	ActiveTraces         int
	AcceptedTotal        uint64
	DedupedTotal         uint64
	CycleSuppressedTotal uint64
}

type TriggerRuntimeSnapshot = RuntimeSnapshot

type Runtime struct {
	mu     sync.Mutex
	config RuntimeConfig
	dedup  map[string]dedupEntry
	cycle  map[string]cycleEntry
	snap   RuntimeSnapshot
}

type TriggerRuntime = Runtime

type dedupEntry struct {
	ReceivedAt        time.Time
	ReplacementPolicy ReplacementPolicy
	TraceID           string
}

type cycleEntry struct {
	LastSeenAt time.Time
	HopCount   uint32
}

func NewRuntime(config RuntimeConfig) *Runtime {
	if config.DedupWindow > TriggerRuntimeMaxDedupWindow {
		config.DedupWindow = TriggerRuntimeMaxDedupWindow
	}
	return &Runtime{config: config, dedup: map[string]dedupEntry{}, cycle: map[string]cycleEntry{}}
}

func NewTriggerRuntime() *TriggerRuntime {
	return NewRuntime(DefaultRuntimeConfig())
}

func NewTriggerRuntimeWithConfig(config TriggerRuntimeConfig) *TriggerRuntime {
	return NewRuntime(config)
}

func (runtime *Runtime) WithConfig(config RuntimeConfig) *Runtime {
	return NewRuntime(config)
}

func (runtime *Runtime) Config() RuntimeConfig { return runtime.config }

func (runtime *Runtime) Snapshot() RuntimeSnapshot {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	snapshot := runtime.snap
	snapshot.DedupEntries = len(runtime.dedup)
	snapshot.ActiveTraces = len(runtime.cycle)
	return snapshot
}

func (runtime *Runtime) DedupEntryCount() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return len(runtime.dedup)
}

func (runtime *Runtime) CycleEntryCount() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return len(runtime.cycle)
}

func (runtime *Runtime) Evaluate(trigger Trigger) EvaluationOutcome {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	now := trigger.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}
	runtime.prune(now)
	if previous, ok := runtime.dedup[trigger.IDempotencyKey]; ok {
		runtime.snap.DedupedTotal = saturatingAddUint64(runtime.snap.DedupedTotal, 1)
		return EvaluationOutcome{Kind: OutcomeDeduped, ReplacementPolicy: previous.ReplacementPolicy, PreviousTraceID: previous.TraceID}
	}
	if existing, ok := runtime.cycle[trigger.TraceID]; ok && existing.HopCount >= runtime.config.CycleHopLimit {
		runtime.snap.CycleSuppressedTotal = saturatingAddUint64(runtime.snap.CycleSuppressedTotal, 1)
		return EvaluationOutcome{Kind: OutcomeCycleSuppressed, HopCount: existing.HopCount}
	}
	runtime.dedup[trigger.IDempotencyKey] = dedupEntry{ReceivedAt: now, ReplacementPolicy: trigger.ReplacementPolicy, TraceID: trigger.TraceID}
	entry := runtime.cycle[trigger.TraceID]
	entry.HopCount = saturatingAddUint32(entry.HopCount, 1)
	entry.LastSeenAt = now
	runtime.cycle[trigger.TraceID] = entry
	runtime.snap.AcceptedTotal = saturatingAddUint64(runtime.snap.AcceptedTotal, 1)
	return EvaluationOutcome{Kind: OutcomeAccept}
}

func (runtime *Runtime) RecordFollowUpHop(traceID string, now time.Time) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.prune(now)
	entry := runtime.cycle[traceID]
	entry.HopCount = saturatingAddUint32(entry.HopCount, 1)
	entry.LastSeenAt = now
	runtime.cycle[traceID] = entry
}

func saturatingAddUint32(value, delta uint32) uint32 {
	if ^uint32(0)-value < delta {
		return ^uint32(0)
	}
	return value + delta
}

func saturatingAddUint64(value, delta uint64) uint64 {
	if ^uint64(0)-value < delta {
		return ^uint64(0)
	}
	return value + delta
}

func (runtime *Runtime) prune(now time.Time) {
	for key, entry := range runtime.dedup {
		if now.Sub(entry.ReceivedAt) > runtime.config.DedupWindow {
			delete(runtime.dedup, key)
		}
	}
	for key, entry := range runtime.cycle {
		if now.Sub(entry.LastSeenAt) > runtime.config.DedupWindow {
			delete(runtime.cycle, key)
		}
	}
}
