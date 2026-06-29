package harness

import (
	"context"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/messages"
	"github.com/detailyang/pig/permission"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/tools"
	"github.com/detailyang/pig/triggers"
)

type Session = session.Session
type MemorySessionRepo = session.MemorySessionRepo
type MemorySessionStorage = session.MemorySessionStorage
type SessionStorage = session.SessionStorage
type SessionTreeEntry = session.SessionTreeEntry
type SessionMetadata = session.SessionMetadata
type JSONLSessionMetadata = session.JSONLMetadata
type JSONlSessionMetadata = session.JSONLMetadata
type JSONLSessionRepo = session.JSONLRepo
type JSONlSessionRepo = session.JSONLRepo
type JSONLSessionStorage = session.JSONLStorage
type JSONlSessionStorage = session.JSONLStorage
type SessionContext = session.SessionContext
type SessionContextModel = session.SessionContextModel
type SessionImportOrigin = session.SessionImportOrigin
type BranchSummaryInput = session.BranchSummaryInput
type ForkOptions = session.ForkOptions
type ForkPosition = session.ForkPosition
type Custom = agent.CustomMessage

type CompactionSettings = compaction.CompactionSettings
type CompactionPreparation = compaction.CompactionPreparation
type CompactionResult = compaction.CompactionResult
type ContextUsageEstimate = compaction.ContextUsageEstimate
type BranchSummaryResult = compaction.BranchSummaryResult
type CutPointResult = compaction.CutPointResult
type GenerateSummaryRequest = compaction.GenerateSummaryRequest
type GenerateSummaryOutput = compaction.GenerateSummaryOutput
type SummarizeError = compaction.SummarizeError

type PermissionDecision = permission.PermissionDecision
type PermissionCategory = permission.PermissionCategory
type PermissionPolicy = permission.PermissionPolicy

const (
	Tool              = permission.Tool
	ControlPlaneWrite = permission.ControlPlaneWrite
)

type TriggerSource = triggers.TriggerSource
type SourceKind = triggers.SourceKind
type PayloadVisibility = triggers.PayloadVisibility
type TriggerAuthority = triggers.TriggerAuthority
type CredentialScope = triggers.CredentialScope
type ReplacementPolicy = triggers.ReplacementPolicy
type TriggerState = triggers.TriggerState
type TriggerRecord = triggers.TriggerRecord
type TriggerRuntimeConfig = triggers.TriggerRuntimeConfig
type TriggerRuntimeSnapshot = triggers.TriggerRuntimeSnapshot
type TriggerRuntime = triggers.TriggerRuntime
type EvaluationOutcome = triggers.EvaluationOutcome

const (
	ForkPositionBefore = session.ForkPositionBefore
	ForkPositionAt     = session.ForkPositionAt

	DEFAULTTURNCONTINUATIONCAP = DEFAULT_TURN_CONTINUATION_CAP

	SourceKindLocal = triggers.SourceKindLocal
	SourceKindMCP   = triggers.SourceKindMCP

	PayloadLocal    = triggers.PayloadLocal
	PayloadShared   = triggers.PayloadShared
	PayloadRedacted = triggers.PayloadRedacted

	CredentialScopeUser    = triggers.CredentialScopeUser
	CredentialScopeProject = triggers.CredentialScopeProject
	CredentialScopeTeam    = triggers.CredentialScopeTeam
	CredentialScopeAgent   = triggers.CredentialScopeAgent
	CredentialScopeNone    = triggers.CredentialScopeNone

	ReplacementPolicyLatestReplaces = triggers.ReplacementPolicyLatestReplaces
	ReplacementPolicyCoalesce       = triggers.ReplacementPolicyCoalesce
	ReplacementPolicyDrop           = triggers.ReplacementPolicyDrop

	TriggerStateReceived         = triggers.StateReceived
	TriggerStateAccepted         = triggers.StateAccepted
	TriggerStateDeduped          = triggers.StateDeduped
	TriggerStateCycleSuppressed  = triggers.StateCycleSuppressed
	TriggerStatePermissionDenied = triggers.StatePermissionDenied
	TriggerStateNeedsApproval    = triggers.StateNeedsApproval
	TriggerStateRunning          = triggers.StateRunning
	TriggerStateCompleted        = triggers.StateCompleted
	TriggerStateFailed           = triggers.StateFailed
)

var DEFAULTCOMPACTIONSETTINGS = compaction.DEFAULT_COMPACTION_SETTINGS
var DEFAULT_COMPACTION_SETTINGS = compaction.DEFAULT_COMPACTION_SETTINGS

const SUMMARIZATIONSYSTEMPROMPT = compaction.SUMMARIZATION_SYSTEM_PROMPT
const SUMMARIZATION_SYSTEM_PROMPT = compaction.SUMMARIZATION_SYSTEM_PROMPT

func CreateSessionID() string { return session.CreateSessionID() }
func CreateTimestamp() string { return session.CreateTimestamp() }
func Uuidv7() string          { return session.Uuidv7() }

func BuildSessionContext(entries []session.Entry) session.Context {
	return session.BuildSessionContext(entries)
}
func ToSession(storage session.Storage) *session.Session { return session.ToSession(storage) }
func GetEntriesToFork(storage session.Storage, options session.ForkOptions) ([]session.Entry, error) {
	return session.GetEntriesToFork(storage, options)
}

func CalculateContextTokens(usage *ai.Usage) uint64 { return compaction.CalculateContextTokens(usage) }
func EstimateTextTokens(text string) uint64         { return compaction.EstimateTextTokens(text) }
func EstimateTokens(message agent.Message) uint64   { return compaction.EstimateTokens(message) }
func BranchSummary(summary string) agent.Message    { return messages.BranchSummary(summary) }
func CompactionSummary(summary string) agent.Message { return messages.CompactionSummary(summary) }
func EstimateContextTokens(messages []agent.Message) compaction.ContextUsageEstimate {
	return compaction.EstimateContextTokens(messages)
}
func ShouldCompact(contextTokens uint64, contextWindow int, settings compaction.Settings) bool {
	return compaction.ShouldCompact(contextTokens, contextWindow, settings)
}
func FindTurnStartIndex(entries []session.Entry, entryIndex int, startIndex int) int {
	return compaction.FindTurnStartIndex(entries, entryIndex, startIndex)
}
func FindCutPoint(entries []session.Entry, settings compaction.Settings) compaction.CutPointResult {
	return compaction.FindCutPoint(entries, settings)
}
func PrepareCompaction(entries []session.Entry, settings compaction.Settings) compaction.Preparation {
	return compaction.PrepareCompaction(entries, settings)
}
func Compact(ctx context.Context, entries []session.Entry, settings compaction.Settings, summarizer compaction.Summarizer) (compaction.Result, error) {
	return compaction.Compact(ctx, entries, settings, summarizer)
}
func SummarizeBranch(ctx context.Context, entries []session.Entry, summarizer compaction.Summarizer) (compaction.BranchSummaryResult, error) {
	return compaction.SummarizeBranch(ctx, entries, summarizer)
}
func SerializeConversation(messages []agent.Message) string {
	return compaction.SerializeConversation(messages)
}
func GetLastAssistantUsage(entries []session.Entry) (*ai.Usage, bool) {
	return compaction.GetLastAssistantUsage(entries)
}
func GenerateSummary(ctx context.Context, request compaction.GenerateSummaryRequest) (compaction.GenerateSummaryOutput, error) {
	return compaction.GenerateSummary(ctx, request)
}
func TruncateText(text string, maxChars int) string { return tools.TruncateText(text, maxChars) }
func TruncateShellOutput(stdout string, stderr string, maxChars int) string {
	return tools.TruncateShellOutput(stdout, stderr, maxChars)
}
func OneLineSummary(snapshot CostSnapshot) string { return CostOneLineSummary(snapshot) }
func FullBreakdown(snapshot CostSnapshot) string  { return CostFullBreakdown(snapshot) }

func Allow() permission.Decision { return permission.Allow() }
func Deny(reason string) permission.Decision {
	return permission.Deny(reason)
}
func DefaultPermissionPolicyForCodingAgent() permission.PermissionPolicy {
	return permission.DefaultPermissionPolicyForCodingAgent()
}
