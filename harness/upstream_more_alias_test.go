package harness

import (
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestHarnessSessionCompactionTriggerAliasesAreAvailable(t *testing.T) {
	var _ Session = Session{}
	var _ MemorySessionRepo = MemorySessionRepo{}
	var _ MemorySessionStorage = MemorySessionStorage{}
	var _ SessionStorage
	var _ SessionTreeEntry = SessionTreeEntry{}
	var _ SessionMetadata = SessionMetadata{}
	var _ SessionContext = BuildSessionContext(nil)
	var _ SessionContextModel = SessionContextModel{}
	var _ SessionImportOrigin = SessionImportOrigin{}
	var _ BranchSummaryInput = BranchSummaryInput{}
	var _ ForkOptions = ForkOptions{Position: ForkPositionAt}
	storage := &MemorySessionStorage{}
	if ToSession(storage) == nil {
		t.Fatal("to session alias mismatch")
	}
	if entries, err := GetEntriesToFork(storage, ForkOptions{}); err != nil || len(entries) != 0 {
		t.Fatalf("get entries to fork alias mismatch entries=%#v err=%v", entries, err)
	}
	if CreateSessionID() == "" || CreateTimestamp() == "" || Uuidv7() == "" {
		t.Fatal("session id/timestamp aliases should return values")
	}

	if DEFAULTCOMPACTIONSETTINGS != DEFAULT_COMPACTION_SETTINGS {
		t.Fatalf("compaction settings alias mismatch")
	}
	if DEFAULTTURNCONTINUATIONCAP != DEFAULT_TURN_CONTINUATION_CAP {
		t.Fatalf("turn continuation alias mismatch")
	}
	var _ CompactionSettings = DEFAULTCOMPACTIONSETTINGS
	var _ CompactionPreparation = PrepareCompaction(nil, DEFAULTCOMPACTIONSETTINGS)
	var _ CompactionResult = CompactionResult{}
	var _ ContextUsageEstimate = EstimateContextTokens(nil)
	var _ BranchSummaryResult = BranchSummaryResult{}
	var _ CutPointResult = FindCutPoint(nil, DEFAULTCOMPACTIONSETTINGS)
	var _ GenerateSummaryRequest = GenerateSummaryRequest{}
	var _ GenerateSummaryOutput = GenerateSummaryOutput{}
	var _ SummarizeError = SummarizeError{}
	var _ Custom = Custom{Role: "notice"}
	branchSummary := BranchSummary("branch note")
	if branchSummary.Custom == nil || branchSummary.Custom.Role != "branch_summary" {
		t.Fatalf("branch summary alias mismatch: %#v", branchSummary)
	}
	compactionSummary := CompactionSummary("compact note")
	if compactionSummary.Custom == nil || compactionSummary.Custom.Role != "compaction_summary" {
		t.Fatalf("compaction summary alias mismatch: %#v", compactionSummary)
	}
	if SUMMARIZATIONSYSTEMPROMPT != SUMMARIZATION_SYSTEM_PROMPT || SUMMARIZATION_SYSTEM_PROMPT == "" {
		t.Fatal("summarization prompt alias mismatch")
	}
	if CalculateContextTokens(&ai.Usage{InputTokens: 1, OutputTokens: 2}) != 3 {
		t.Fatal("context token alias mismatch")
	}
	if EstimateTextTokens("abcd") != 1 || EstimateTokens(agent.NewUserMessage("hello")) == 0 {
		t.Fatal("token estimate alias mismatch")
	}
	if !ShouldCompact(100, 100, DEFAULTCOMPACTIONSETTINGS) {
		t.Fatal("should compact alias mismatch")
	}
	if FindTurnStartIndex(nil, 0, 0) != 0 || TruncateText("abcd", 2) == "abcd" || TruncateShellOutput("abcd", "", 2) == "abcd" {
		t.Fatal("utility alias mismatch")
	}

	var _ PermissionCategory = PermissionCategory(Tool)
	var _ PermissionDecision = PermissionDecision(Allow())
	var _ PermissionPolicy = DefaultPermissionPolicyForCodingAgent()

	var _ TriggerSource = TriggerSource{}
	var _ SourceKind = SourceKindLocal
	var _ PayloadVisibility = PayloadShared
	var _ TriggerAuthority = TriggerAuthority{CredentialScope: CredentialScopeNone}
	var _ CredentialScope = CredentialScopeUser
	var _ ReplacementPolicy = ReplacementPolicyDrop
	var _ TriggerState = TriggerStateAccepted
	var _ TriggerRecord = TriggerRecord{}
	var _ TriggerRuntimeConfig = TriggerRuntimeConfig{}
	var _ TriggerRuntimeSnapshot = TriggerRuntimeSnapshot{}
	var _ TriggerRuntime = TriggerRuntime{}
	var _ EvaluationOutcome = EvaluationOutcome{}
}
