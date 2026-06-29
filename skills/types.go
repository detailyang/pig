package skills

type Source string

type SkillSource = Source

const (
	SourceBuiltin Source = "builtin"
	SourceUser    Source = "user"
	SourceProject Source = "project"

	SkillSourceBuiltin = SourceBuiltin
	SkillSourceUser    = SourceUser
	SkillSourceProject = SourceProject
)

type Skill struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	FilePath               string `json:"filePath"`
	Content                string `json:"content"`
	DisableModelInvocation bool   `json:"disableModelInvocation"`
	Source                 Source `json:"source"`
}

type Frontmatter struct {
	Name                   string
	HasName                bool
	Description            string
	DisableModelInvocation bool
}

type SkillFrontmatter = Frontmatter

type DiagnosticCode string

type SkillDiagnosticCode = DiagnosticCode

const (
	DiagnosticFileInfoFailed  DiagnosticCode = "file_info_failed"
	DiagnosticListFailed      DiagnosticCode = "list_failed"
	DiagnosticReadFailed      DiagnosticCode = "read_failed"
	DiagnosticParseFailed     DiagnosticCode = "parse_failed"
	DiagnosticInvalidMetadata DiagnosticCode = "invalid_metadata"

	SkillDiagnosticFileInfoFailed  = DiagnosticFileInfoFailed
	SkillDiagnosticListFailed      = DiagnosticListFailed
	SkillDiagnosticReadFailed      = DiagnosticReadFailed
	SkillDiagnosticParseFailed     = DiagnosticParseFailed
	SkillDiagnosticInvalidMetadata = DiagnosticInvalidMetadata
)

type Diagnostic struct {
	Code    DiagnosticCode `json:"code"`
	Message string         `json:"message"`
	Path    string         `json:"path"`
}

type SkillDiagnostic = Diagnostic

type LoadOutput struct {
	Skills      []Skill
	Diagnostics []Diagnostic
}

type LoadSkillsOutput = LoadOutput

type SourcedInput struct {
	Dir    string
	Source Source
}

type SourcedSkill struct {
	Skill       Skill
	Source      Source
	Diagnostics []Diagnostic
}

func (source Source) Label() string { return string(source) }

func (skill Skill) MarshalJSON() ([]byte, error) {
	type alias Skill
	if skill.Source == "" {
		skill.Source = SourceUser
	}
	return marshalJSONNoHTMLEscape(alias(skill))
}
