package harness

import (
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/templates"
)

type SkillSource = skills.SkillSource
type Skill = skills.Skill
type SkillFrontmatter = skills.SkillFrontmatter
type SkillDiagnosticCode = skills.SkillDiagnosticCode
type SkillDiagnostic = skills.SkillDiagnostic
type LoadSkillsOutput = skills.LoadSkillsOutput
type PromptTemplate = templates.PromptTemplate
type CompactionErrorCode = compaction.CompactionErrorCode
type CompactionError = compaction.CompactionError
type SessionErrorCode = session.SessionErrorCode
type SessionError = session.SessionError

const (
	SkillSourceBuiltin = skills.SkillSourceBuiltin
	SkillSourceUser    = skills.SkillSourceUser
	SkillSourceProject = skills.SkillSourceProject

	SkillDiagnosticFileInfoFailed  = skills.SkillDiagnosticFileInfoFailed
	SkillDiagnosticListFailed      = skills.SkillDiagnosticListFailed
	SkillDiagnosticReadFailed      = skills.SkillDiagnosticReadFailed
	SkillDiagnosticParseFailed     = skills.SkillDiagnosticParseFailed
	SkillDiagnosticInvalidMetadata = skills.SkillDiagnosticInvalidMetadata

	CompactionErrorAborted             = compaction.CompactionErrorAborted
	CompactionErrorSummarizationFailed = compaction.CompactionErrorSummarizationFailed
	CompactionErrorInvalidSession      = compaction.CompactionErrorInvalidSession
	CompactionErrorUnknown             = compaction.CompactionErrorUnknown

	SessionErrorNotFound       = session.SessionErrorNotFound
	SessionErrorAlreadyExists  = session.SessionErrorAlreadyExists
	SessionErrorCorrupted      = session.SessionErrorCorrupted
	SessionErrorStorageFailure = session.SessionErrorStorageFailure
	SessionErrorAborted        = session.SessionErrorAborted
	SessionErrorUnknown        = session.SessionErrorUnknown
)
