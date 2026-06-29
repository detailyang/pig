package triggers

import "testing"

func TestTriggerRuntimeUpstreamOutcomeAliasesAreAvailable(t *testing.T) {
	if EvaluationOutcomeAccept != OutcomeAccept || EvaluationOutcomeDeduped != OutcomeDeduped || EvaluationOutcomeCycleSuppressed != OutcomeCycleSuppressed {
		t.Fatalf("evaluation outcome aliases mismatch")
	}
}

func TestTriggerUpstreamEnumVariantAliases(t *testing.T) {
	if TriggerSourceMcp != SourceMCP || TriggerSourceLocal != SourceLocal || TriggerSourceAgentDelegate != SourceAgentDelegate {
		t.Fatalf("trigger source aliases mismatch")
	}
	if PayloadVisibilityLocal != PayloadLocal || PayloadVisibilityShared != PayloadShared || PayloadVisibilityRedacted != PayloadRedacted {
		t.Fatalf("payload visibility aliases mismatch")
	}
	if ReplacementPolicyLatestReplaces != ReplacementLatestReplaces || ReplacementPolicyCoalesce != ReplacementCoalesce || ReplacementPolicyDrop != ReplacementDrop {
		t.Fatalf("replacement policy aliases mismatch")
	}
	if CredentialScopeUser != ScopeUser || CredentialScopeProject != ScopeProject || CredentialScopeTeam != ScopeTeam || CredentialScopeAgent != ScopeAgent || CredentialScopeNone != ScopeNone {
		t.Fatalf("credential scope aliases mismatch")
	}
	if TriggerStateReceived != StateReceived || TriggerStateAccepted != StateAccepted || TriggerStateDeduped != StateDeduped || TriggerStateCycleSuppressed != StateCycleSuppressed || TriggerStatePermissionDenied != StatePermissionDenied || TriggerStateNeedsApproval != StateNeedsApproval || TriggerStateRunning != StateRunning || TriggerStateFailed != StateFailed || TriggerStateCompleted != StateCompleted {
		t.Fatalf("trigger state aliases mismatch")
	}
}
