package session

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/triggers"
)

const archiveSchema = "pie.session_export.v1"
const archiveManifestPath = "manifest.json"
const archiveSessionPath = "session.jsonl"
const archiveTriggersPath = "sidecars/triggers.json"
const archiveCronPath = "sidecars/cron.toml"
const maxArchiveManifestBytes = 128 * 1024
const maxArchiveSessionBytes = 50 * 1024 * 1024
const maxArchiveSidecarBytes = 2 * 1024 * 1024

type ExportSummary struct {
	OutputPath  string
	SessionID   string
	EntryCount  int
	HasTriggers bool
	HasCron     bool
}

type ExportArchiveOptions struct {
	ExcludeTriggers bool
}

type ImportSummary struct {
	SessionID                 string
	SessionPath               string
	EntryCount                int
	TriggersImported          int
	CronImported              int
	AutomationEnabled         bool
	OriginallyEnabledTriggers []string
	OriginallyEnabledCron     []string
}

type ImportArchiveOptions struct {
	Activation                  AutomationActivation
	ActivateAutomation          bool
	ConfirmAutomationActivation func(AutomationActivationSummary) (bool, error)
}

type AutomationActivationSummary struct {
	Triggers                  int
	Cron                      int
	OriginallyEnabledTriggers []string
	OriginallyEnabledCron     []string
}

type AutomationActivation string

const (
	AutomationActivationOff AutomationActivation = "off"
	AutomationActivationAsk AutomationActivation = "ask"
	AutomationActivationOn  AutomationActivation = "on"
)

type ActivateTriggers = AutomationActivation

const (
	ActivateTriggersOff ActivateTriggers = AutomationActivationOff
	ActivateTriggersAsk ActivateTriggers = AutomationActivationAsk
	ActivateTriggersOn  ActivateTriggers = AutomationActivationOn
)

func TriggerSidecarPath(sessionPath string) string {
	return strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".triggers.json"
}

func CronSidecarPath(sessionPath string) string {
	return strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".cron.toml"
}

func EndpointSidecarPath(sessionPath string) string {
	return strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".endpoints.json"
}

func TriggerSidecarPathForSession(session *Session, repo *JSONLRepo) (string, error) {
	return sidecarPathForSession(session, repo, TriggerSidecarPath, ".triggers.json")
}

func CronSidecarPathForSession(session *Session, repo *JSONLRepo) (string, error) {
	return sidecarPathForSession(session, repo, CronSidecarPath, ".cron.toml")
}

func sidecarPathForSession(session *Session, repo *JSONLRepo, pathFor func(string) string, suffix string) (string, error) {
	metadata, err := session.Storage().MetadataJSON()
	if err != nil {
		return "", err
	}
	if path, ok := metadata["path"].(string); ok && path != "" {
		return pathFor(path), nil
	}
	id, _ := metadata["id"].(string)
	if id == "" {
		id = "unknown-session"
	}
	return filepath.Join(repo.Root(), id+suffix), nil
}

func DefaultExportPath(cwd, sessionID string) string {
	shortRunes := []rune(sessionID)
	if len(shortRunes) > 16 {
		shortRunes = shortRunes[:16]
	}
	short := string(shortRunes)
	return filepath.Join(cwd, "pie-session-"+short+".piesession")
}

type archiveManifest struct {
	Schema      string                     `json:"schema"`
	CreatedAt   string                     `json:"created_at"`
	PieVersion  string                     `json:"pie_version"`
	Source      archiveManifestSource      `json:"source"`
	Content     archiveManifestContent     `json:"content"`
	Sensitivity archiveManifestSensitivity `json:"sensitivity"`
}

type archiveManifestSource struct {
	SessionID   string `json:"session_id"`
	CWD         string `json:"cwd"`
	SessionPath string `json:"session_path"`
}

type archiveManifestContent struct {
	SessionJSONLSHA256 string  `json:"session_jsonl_sha256"`
	EntryCount         int     `json:"entry_count"`
	ActiveLeafID       *string `json:"active_leaf_id"`
	HasTriggers        bool    `json:"has_triggers"`
	HasCron            bool    `json:"has_cron"`
}

type archiveManifestSensitivity struct {
	SessionTranscriptPreserved  bool `json:"session_transcript_preserved"`
	SeparateAuthStoresIncluded  bool `json:"separate_auth_stores_included"`
	ProviderCredentialsIncluded bool `json:"provider_credentials_included"`
	MCPConfigIncluded           bool `json:"mcp_config_included"`
}

type ParsedSession struct {
	Metadata           JSONLMetadata
	Entries            []Entry
	OriginalEntryLines []string
	ActiveLeafID       *string
}

type RewrittenSidecar struct {
	Bytes      []byte
	Count      int
	EnabledIDs []string
}

type ArchiveManifestForImport struct {
	SourceSessionID string
	SourceCWD       string
	CreatedAt       string
	PieVersion      string
}

func ExportArchive(sessionPath, outputPath, pieVersion string) (ExportSummary, error) {
	return ExportArchiveWithOptions(sessionPath, outputPath, pieVersion, ExportArchiveOptions{})
}

func ExportSession(sessionPath, outputPath string, excludeTriggers bool, pieVersion string) (ExportSummary, error) {
	return ExportArchiveWithOptions(sessionPath, outputPath, pieVersion, ExportArchiveOptions{ExcludeTriggers: excludeTriggers})
}

func ExportArchiveWithOptions(sessionPath, outputPath, pieVersion string, options ExportArchiveOptions) (ExportSummary, error) {
	sessionBytes, err := os.ReadFile(sessionPath)
	if err != nil {
		return ExportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if len(sessionBytes) > maxArchiveSessionBytes {
		return ExportSummary{}, Error{Code: ErrorStorageFailure, Message: "session transcript is too large to export"}
	}
	if !utf8.Valid(sessionBytes) {
		return ExportSummary{}, Error{Code: ErrorStorageFailure, Message: "session transcript is invalid UTF-8"}
	}
	var triggerBytes, cronBytes []byte
	var hasTriggers, hasCron bool
	if !options.ExcludeTriggers {
		var err error
		triggerBytes, hasTriggers, err = readOptionalArchiveSidecar(TriggerSidecarPath(sessionPath))
		if err != nil {
			return ExportSummary{}, err
		}
		cronBytes, hasCron, err = readOptionalArchiveSidecar(CronSidecarPath(sessionPath))
		if err != nil {
			return ExportSummary{}, err
		}
	}
	parsed, err := parseJSONLTranscript(sessionBytes)
	if err != nil {
		return ExportSummary{}, err
	}
	manifest := archiveManifest{
		Schema:     archiveSchema,
		CreatedAt:  CreateTimestamp(),
		PieVersion: pieVersion,
		Source: archiveManifestSource{
			SessionID:   parsed.metadata.ID,
			CWD:         parsed.metadata.CWD,
			SessionPath: parsed.metadata.Path,
		},
		Content: archiveManifestContent{
			SessionJSONLSHA256: sha256Hex(sessionBytes),
			EntryCount:         len(parsed.entries),
			ActiveLeafID:       parsed.activeLeafID,
			HasTriggers:        hasTriggers,
			HasCron:            hasCron,
		},
		Sensitivity: archiveManifestSensitivity{SessionTranscriptPreserved: true},
	}
	manifestBytes, err := marshalJSONIndentNoHTMLEscape(manifest, "", "  ")
	if err != nil {
		return ExportSummary{}, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return ExportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	file, err := CreateArchiveFile(outputPath)
	if err != nil {
		return ExportSummary{}, err
	}
	defer file.Close()
	writer := tar.NewWriter(file)
	if err := writeTarFile(writer, archiveManifestPath, manifestBytes); err != nil {
		return ExportSummary{}, err
	}
	if err := writeTarFile(writer, archiveSessionPath, sessionBytes); err != nil {
		return ExportSummary{}, err
	}
	if hasTriggers {
		if err := writeTarFile(writer, archiveTriggersPath, triggerBytes); err != nil {
			return ExportSummary{}, err
		}
	}
	if hasCron {
		if err := writeTarFile(writer, archiveCronPath, cronBytes); err != nil {
			return ExportSummary{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return ExportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return ExportSummary{OutputPath: outputPath, SessionID: parsed.metadata.ID, EntryCount: len(parsed.entries), HasTriggers: hasTriggers, HasCron: hasCron}, nil
}

func ImportArchive(repo *JSONLRepo, archivePath, cwd string) (ImportSummary, error) {
	return ImportArchiveWithOptions(repo, archivePath, cwd, ImportArchiveOptions{})
}

func CreateArchiveFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return file, nil
	}
	if os.IsExist(err) {
		return nil, Error{Code: ErrorAlreadyExists, Message: "output already exists: " + path + " (remove it or pass a different path)"}
	}
	return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
}

func ParseSessionJSONL(text string) (ParsedSession, error) {
	parsed, err := parseJSONLTranscript([]byte(text))
	if err != nil {
		return ParsedSession{}, err
	}
	return exportParsedSession(parsed), nil
}

func ReadArchive(path string) (map[string][]byte, error) {
	return readTarArchive(path)
}

func ReadOptionalSidecar(path string) ([]byte, bool, error) {
	return readOptionalArchiveSidecar(path)
}

func ValidateArchivePath(path string) error {
	if safeArchivePath(path) {
		return nil
	}
	return Error{Code: ErrorCorrupted, Message: "session archive contains an unsafe path"}
}

func RewriteTriggerSidecar(data []byte, activate bool) (RewrittenSidecar, error) {
	sidecar, err := rewriteTriggerSidecar(data, activate)
	if err != nil {
		return RewrittenSidecar{}, err
	}
	return exportRewrittenSidecar(sidecar), nil
}

func RewriteCronSidecar(data []byte, activate bool) (RewrittenSidecar, error) {
	sidecar, err := rewriteCronSidecar(data, activate)
	if err != nil {
		return RewrittenSidecar{}, err
	}
	return exportRewrittenSidecar(sidecar), nil
}

func RewriteSessionJSONL(parsed ParsedSession, manifest ArchiveManifestForImport, newID, cwd, path string) (string, error) {
	metadata := JSONLMetadata{Metadata: Metadata{ID: newID, CreatedAt: CreateTimestamp()}, CWD: cwd, Path: path, ImportedFrom: &SessionImportOrigin{SessionID: manifest.SourceSessionID, CWD: manifest.SourceCWD, ExportedAt: manifest.CreatedAt, PieVersion: manifest.PieVersion}}
	return renderImportedSessionJSONL(metadata, parsed.OriginalEntryLines)
}

func AppendBytes(writer *tar.Writer, path string, data []byte) error {
	return writeTarFile(writer, path, data)
}

func CommitImport(repo *JSONLRepo, sessionPath, tempPath, sessionContent string, sidecars map[string][]byte) error {
	return commitImport(repo, sessionPath, tempPath, sessionContent, sidecars)
}

func ImportSession(repo *JSONLRepo, archivePath, cwd string, activateTriggers ActivateTriggers) (ImportSummary, error) {
	return ImportArchiveWithOptions(repo, archivePath, cwd, ImportArchiveOptions{Activation: activateTriggers})
}

func ImportArchiveWithOptions(repo *JSONLRepo, archivePath, cwd string, options ImportArchiveOptions) (ImportSummary, error) {
	activation := options.resolvedActivation()
	automationEnabled := activation == AutomationActivationOn
	files, err := readTarArchive(archivePath)
	if err != nil {
		return ImportSummary{}, err
	}
	manifestBytes, ok := files[archiveManifestPath]
	if !ok {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session archive is missing manifest.json"}
	}
	sessionBytes, ok := files[archiveSessionPath]
	if !ok {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session archive is missing session.jsonl"}
	}
	var manifest archiveManifest
	if !utf8.Valid(manifestBytes) {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "parse session archive manifest: invalid UTF-8"}
	}
	if hasJSONLoneSurrogateEscape(manifestBytes) {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "parse session archive manifest: invalid Unicode escape"}
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "parse session archive manifest: " + err.Error()}
	}
	if manifest.Schema != archiveSchema {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "unsupported session archive schema"}
	}
	if sha256Hex(sessionBytes) != manifest.Content.SessionJSONLSHA256 {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session archive checksum mismatch"}
	}
	if !utf8.Valid(sessionBytes) {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session.jsonl is not UTF-8"}
	}
	parsed, err := parseJSONLTranscript(sessionBytes)
	if err != nil {
		return ImportSummary{}, err
	}
	if len(parsed.entries) != manifest.Content.EntryCount {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session archive entry count mismatch"}
	}
	if !sameStringPtr(parsed.activeLeafID, manifest.Content.ActiveLeafID) {
		return ImportSummary{}, Error{Code: ErrorCorrupted, Message: "session archive active leaf mismatch"}
	}
	triggerSidecar, err := parseImportedTriggerSidecar(files, automationEnabled)
	if err != nil {
		return ImportSummary{}, err
	}
	cronSidecar, err := parseImportedCronSidecar(files, automationEnabled)
	if err != nil {
		return ImportSummary{}, err
	}
	if activation == AutomationActivationAsk {
		if options.ConfirmAutomationActivation == nil {
			return ImportSummary{}, Error{Code: ErrorStorageFailure, Message: "activate-triggers=ask requires interactive confirmation and is not implemented yet; use off or on"}
		}
		enabled, err := options.ConfirmAutomationActivation(AutomationActivationSummary{Triggers: triggerSidecar.count, Cron: cronSidecar.count, OriginallyEnabledTriggers: append([]string(nil), triggerSidecar.enabledIDs...), OriginallyEnabledCron: append([]string(nil), cronSidecar.enabledIDs...)})
		if err != nil {
			return ImportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
		}
		automationEnabled = enabled
		triggerSidecar, err = parseImportedTriggerSidecar(files, automationEnabled)
		if err != nil {
			return ImportSummary{}, err
		}
		cronSidecar, err = parseImportedCronSidecar(files, automationEnabled)
		if err != nil {
			return ImportSummary{}, err
		}
	}
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return ImportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	newID := CreateSessionID()
	sessionPath := filepath.Join(repo.root, newID+".jsonl")
	if _, err := os.Stat(sessionPath); err == nil {
		return ImportSummary{}, Error{Code: ErrorStorageFailure, Message: "import destination already exists"}
	} else if !os.IsNotExist(err) {
		return ImportSummary{}, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	metadata := JSONLMetadata{Metadata: Metadata{ID: newID, CreatedAt: CreateTimestamp()}, CWD: cwd, Path: sessionPath, ImportedFrom: &SessionImportOrigin{SessionID: manifest.Source.SessionID, CWD: manifest.Source.CWD, ExportedAt: manifest.CreatedAt, PieVersion: manifest.PieVersion}}
	sidecars := map[string][]byte{}
	if triggerSidecar.present {
		sidecars[TriggerSidecarPath(sessionPath)] = triggerSidecar.bytes
	}
	if cronSidecar.present {
		sidecars[CronSidecarPath(sessionPath)] = cronSidecar.bytes
	}
	if err := commitImportedArchive(repo, sessionPath, sessionPath+".tmp", metadata, parsed, sidecars); err != nil {
		return ImportSummary{}, err
	}
	return ImportSummary{SessionID: newID, SessionPath: sessionPath, EntryCount: len(parsed.entries), TriggersImported: triggerSidecar.count, CronImported: cronSidecar.count, AutomationEnabled: automationEnabled, OriginallyEnabledTriggers: triggerSidecar.enabledIDs, OriginallyEnabledCron: cronSidecar.enabledIDs}, nil
}

func (options ImportArchiveOptions) resolvedActivation() AutomationActivation {
	if options.Activation != "" {
		return options.Activation
	}
	if options.ActivateAutomation {
		return AutomationActivationOn
	}
	return AutomationActivationOff
}

func ActivateImported(sessionPath string, triggerIDs, cronIDs []string) (int, int, error) {
	triggersEnabled := 0
	if len(triggerIDs) > 0 {
		path := TriggerSidecarPath(sessionPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, Error{Code: ErrorStorageFailure, Message: err.Error()}
		}
		if !utf8.Valid(data) {
			return 0, 0, Error{Code: ErrorStorageFailure, Message: "stream did not contain valid UTF-8"}
		}
		if hasJSONLoneSurrogateEscape(data) {
			return 0, 0, Error{Code: ErrorCorrupted, Message: "parse trigger sidecar: invalid Unicode escape"}
		}
		var file struct {
			Version uint32                 `json:"version"`
			Rules   []triggers.DynamicRule `json:"rules"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			return 0, 0, Error{Code: ErrorCorrupted, Message: "parse trigger sidecar: " + err.Error()}
		}
		want := stringSet(triggerIDs)
		for index := range file.Rules {
			if want[file.Rules[index].ID] && !file.Rules[index].Enabled {
				file.Rules[index].Enabled = true
				triggersEnabled++
			}
		}
		data, err = marshalJSONIndentNoHTMLEscape(file, "", "  ")
		if err != nil {
			return 0, 0, Error{Code: ErrorCorrupted, Message: err.Error()}
		}
		if err := writeImportedSidecar(path, data); err != nil {
			return 0, 0, err
		}
	}

	cronEnabled := 0
	if len(cronIDs) > 0 {
		path := CronSidecarPath(sessionPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, Error{Code: ErrorStorageFailure, Message: err.Error()}
		}
		if !utf8.Valid(data) {
			return 0, 0, Error{Code: ErrorStorageFailure, Message: "stream did not contain valid UTF-8"}
		}
		file, err := parseCronTOML(data)
		if err != nil {
			return 0, 0, Error{Code: ErrorCorrupted, Message: "parse cron sidecar: " + err.Error()}
		}
		want := stringSet(cronIDs)
		for index := range file.jobs {
			if want[file.jobs[index].id] && !file.jobs[index].enabled {
				file.jobs[index].enabled = true
				cronEnabled++
			}
			for key := range file.jobs[index].fields {
				if !isKnownCronTOMLField(key) {
					delete(file.jobs[index].fields, key)
				}
			}
		}
		data = []byte(formatCronTOML(file))
		if err := writeImportedSidecar(path, data); err != nil {
			return 0, 0, err
		}
	}
	return triggersEnabled, cronEnabled, nil
}

type importedSidecar struct {
	present    bool
	bytes      []byte
	count      int
	enabledIDs []string
}

func readOptionalArchiveSidecar(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if len(data) > maxArchiveSidecarBytes {
		return nil, false, Error{Code: ErrorStorageFailure, Message: "session sidecar is too large to export"}
	}
	return data, true, nil
}

func parseImportedTriggerSidecar(files map[string][]byte, activate bool) (importedSidecar, error) {
	data, ok := files[archiveTriggersPath]
	if !ok {
		return importedSidecar{}, nil
	}
	return rewriteTriggerSidecar(data, activate)
}

func rewriteTriggerSidecar(data []byte, activate bool) (importedSidecar, error) {
	if !utf8.Valid(data) {
		return importedSidecar{}, Error{Code: ErrorCorrupted, Message: "parse trigger sidecar: invalid UTF-8"}
	}
	if hasJSONLoneSurrogateEscape(data) {
		return importedSidecar{}, Error{Code: ErrorCorrupted, Message: "parse trigger sidecar: invalid Unicode escape"}
	}
	var file struct {
		Version uint32                 `json:"version"`
		Rules   []triggers.DynamicRule `json:"rules"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return importedSidecar{}, Error{Code: ErrorCorrupted, Message: "parse trigger sidecar: " + err.Error()}
	}
	var enabledIDs []string
	for index := range file.Rules {
		if file.Rules[index].Enabled {
			enabledIDs = append(enabledIDs, file.Rules[index].ID)
		}
		file.Rules[index].Enabled = file.Rules[index].Enabled && activate
	}
	rewritten, err := marshalJSONIndentNoHTMLEscape(file, "", "  ")
	if err != nil {
		return importedSidecar{}, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	return importedSidecar{present: true, bytes: rewritten, count: len(file.Rules), enabledIDs: enabledIDs}, nil
}

func parseImportedCronSidecar(files map[string][]byte, activate bool) (importedSidecar, error) {
	data, ok := files[archiveCronPath]
	if !ok {
		return importedSidecar{}, nil
	}
	return rewriteCronSidecar(data, activate)
}

func rewriteCronSidecar(data []byte, activate bool) (importedSidecar, error) {
	return parseImportedCronTOMLSidecar(data, activate)
}

func parseImportedCronTOMLSidecar(data []byte, activate bool) (importedSidecar, error) {
	file, err := parseCronTOML(data)
	if err != nil {
		return importedSidecar{}, Error{Code: ErrorCorrupted, Message: "parse cron sidecar: " + err.Error()}
	}
	var enabledIDs []string
	for index := range file.jobs {
		if file.jobs[index].enabled {
			enabledIDs = append(enabledIDs, file.jobs[index].id)
		}
		file.jobs[index].enabled = file.jobs[index].enabled && activate
		for key := range file.jobs[index].fields {
			if !isKnownCronTOMLField(key) {
				delete(file.jobs[index].fields, key)
			}
		}
		delete(file.jobs[index].fields, "running_trace_id")
		delete(file.jobs[index].fields, "last_due_at")
		delete(file.jobs[index].fields, "last_error")
		delete(file.jobs[index].fields, "skipped_overlap_count")
	}
	return importedSidecar{present: true, bytes: []byte(formatCronTOML(file)), count: len(file.jobs), enabledIDs: enabledIDs}, nil
}

type cronTOMLFile struct {
	jobs []cronTOMLJob
}

type cronTOMLJob struct {
	id      string
	enabled bool
	fields  map[string]string
	order   []string
}

func parseCronTOML(data []byte) (cronTOMLFile, error) {
	if !utf8.Valid(data) {
		return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar is not UTF-8"}
	}
	lines := strings.Split(string(data), "\n")
	var file cronTOMLFile
	var current *cronTOMLJob
	for _, raw := range lines {
		line := strings.TrimSpace(stripTOMLInlineComment(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isCronJobsArrayHeader(line) {
			file.jobs = append(file.jobs, cronTOMLJob{fields: map[string]string{}})
			current = &file.jobs[len(file.jobs)-1]
			continue
		}
		if current == nil {
			return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar field appears before [[jobs]]"}
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "invalid cron sidecar line"}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "invalid cron sidecar key"}
		}
		if _, exists := current.fields[key]; exists {
			return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar duplicate key: " + key}
		}
		current.order = append(current.order, key)
		current.fields[key] = value
		switch key {
		case "id":
			current.id = trimTOMLString(value)
		case "enabled":
			current.enabled = value == "true"
		}
	}
	for _, job := range file.jobs {
		for _, key := range []string{"id", "schedule", "action", "enabled", "created_at"} {
			if _, ok := job.fields[key]; !ok {
				return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar missing required field: " + key}
			}
		}
		for _, key := range []string{"id", "schedule", "action", "created_at", "running_trace_id", "last_due_at", "last_fired_at", "last_completed_at", "last_error"} {
			value, ok := job.fields[key]
			if !ok {
				continue
			}
			if !isTOMLString(value) || !hasValidTOMLStringEscapes(value) {
				return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar invalid string field: " + key}
			}
		}
		for _, key := range []string{"created_at", "last_due_at", "last_fired_at", "last_completed_at"} {
			value, ok := job.fields[key]
			if !ok {
				continue
			}
			if _, err := time.Parse(time.RFC3339, trimTOMLString(value)); err != nil {
				return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar invalid datetime field: " + key}
			}
		}
		for _, key := range []string{"enabled", "stateful"} {
			value, ok := job.fields[key]
			if !ok {
				continue
			}
			if value != "true" && value != "false" {
				return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar invalid boolean field: " + key}
			}
		}
		if value, ok := job.fields["skipped_overlap_count"]; ok {
			if _, err := parseTOMLUint(value); err != nil {
				return cronTOMLFile{}, Error{Code: ErrorCorrupted, Message: "cron sidecar invalid integer field: skipped_overlap_count"}
			}
		}
	}
	return file, nil
}

func isTOMLString(value string) bool {
	return len(value) >= 2 && ((strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) || (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")))
}

func trimTOMLString(value string) string {
	if isTOMLString(value) {
		return value[1 : len(value)-1]
	}
	return value
}

func isCronJobsArrayHeader(line string) bool {
	if !strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]]") {
		return false
	}
	name := strings.Trim(line[2:len(line)-2], " \t")
	return name == "jobs"
}

func stripTOMLInlineComment(line string) string {
	inBasicString := false
	inLiteralString := false
	escaped := false
	for index, ch := range line {
		if escaped {
			escaped = false
			continue
		}
		if inBasicString && ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' && !inLiteralString {
			inBasicString = !inBasicString
			continue
		}
		if ch == '\'' && !inBasicString {
			inLiteralString = !inLiteralString
			continue
		}
		if ch == '#' && !inBasicString && !inLiteralString {
			return line[:index]
		}
	}
	return line
}

func hasValidTOMLStringEscapes(value string) bool {
	if strings.HasPrefix(value, "'") {
		return true
	}
	for index := 1; index < len(value)-1; index++ {
		if value[index] != '\\' {
			continue
		}
		index++
		if index >= len(value)-1 {
			return false
		}
		switch value[index] {
		case 'b', 't', 'n', 'f', 'r', '"', '\\':
		case 'u':
			if index+4 >= len(value)-1 || !isHexString(value[index+1:index+5]) {
				return false
			}
			codepoint, ok := parseHexCodepoint(value[index+1 : index+5])
			if !ok || !isValidUnicodeScalar(codepoint) {
				return false
			}
			index += 4
		case 'U':
			if index+8 >= len(value)-1 || !isHexString(value[index+1:index+9]) {
				return false
			}
			codepoint, ok := parseHexCodepoint(value[index+1 : index+9])
			if !ok || !isValidUnicodeScalar(codepoint) {
				return false
			}
			index += 8
		default:
			return false
		}
	}
	return true
}

func isHexString(value string) bool {
	for _, ch := range value {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return value != ""
}

func parseHexCodepoint(value string) (uint32, bool) {
	var codepoint uint32
	for _, ch := range value {
		var digit uint32
		switch {
		case ch >= '0' && ch <= '9':
			digit = uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			digit = uint32(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			digit = uint32(ch-'A') + 10
		default:
			return 0, false
		}
		codepoint = codepoint<<4 | digit
	}
	return codepoint, true
}

func isValidUnicodeScalar(codepoint uint32) bool {
	return codepoint <= 0x10ffff && (codepoint < 0xd800 || codepoint > 0xdfff)
}

func parseTOMLUint(value string) (uint64, error) {
	trimmed := strings.ReplaceAll(value, "_", "")
	if trimmed == "" {
		return 0, strconv.ErrSyntax
	}
	base := 10
	if len(trimmed) > 2 && trimmed[0] == '0' {
		switch trimmed[1] {
		case 'x', 'X':
			base = 16
			trimmed = trimmed[2:]
		case 'o', 'O':
			base = 8
			trimmed = trimmed[2:]
		case 'b', 'B':
			base = 2
			trimmed = trimmed[2:]
		}
	}
	if trimmed == "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseUint(trimmed, base, 64)
}

func formatCronTOML(file cronTOMLFile) string {
	var builder strings.Builder
	for jobIndex, job := range file.jobs {
		if jobIndex > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("[[jobs]]\n")
		job.fields["enabled"] = boolTOML(job.enabled)
		order := append([]string(nil), job.order...)
		if _, ok := job.fields["enabled"]; ok && !containsString(order, "enabled") {
			order = append(order, "enabled")
		}
		for _, key := range order {
			value, ok := job.fields[key]
			if !ok {
				continue
			}
			builder.WriteString(key)
			builder.WriteString(" = ")
			builder.WriteString(value)
			builder.WriteByte('\n')
		}
		var extras []string
		for key := range job.fields {
			if !containsString(order, key) {
				extras = append(extras, key)
			}
		}
		sort.Strings(extras)
		for _, key := range extras {
			builder.WriteString(key)
			builder.WriteString(" = ")
			builder.WriteString(job.fields[key])
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func isKnownCronTOMLField(key string) bool {
	switch key {
	case "id", "schedule", "action", "enabled", "running_trace_id", "last_due_at", "last_fired_at", "last_completed_at", "last_error", "skipped_overlap_count", "stateful", "created_at":
		return true
	default:
		return false
	}
}

func boolTOML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeImportedSidecar(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return nil
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func commitImportedArchive(repo *JSONLRepo, path, tmpPath string, metadata JSONLMetadata, parsed parsedJSONLTranscript, sidecars map[string][]byte) error {
	sessionContent, err := renderImportedSessionJSONL(metadata, parsed.entryLines)
	if err != nil {
		return err
	}
	return commitImport(repo, path, tmpPath, sessionContent, sidecars)
}

func commitImport(repo *JSONLRepo, path, tmpPath, sessionContent string, sidecars map[string][]byte) error {
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if err := writeImportedSessionFile(tmpPath, sessionContent); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := OpenJSONLStorage(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	writtenSidecars := make([]string, 0, len(sidecars))
	for sidecarPath, data := range sidecars {
		if err := writeImportedSidecar(sidecarPath, data); err != nil {
			_ = os.Remove(tmpPath)
			for _, written := range writtenSidecars {
				_ = os.Remove(written)
			}
			return err
		}
		writtenSidecars = append(writtenSidecars, sidecarPath)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		for _, written := range writtenSidecars {
			_ = os.Remove(written)
		}
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return nil
}

func writeImportedSessionFile(path string, text string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return nil
}

func renderImportedSessionJSONL(metadata JSONLMetadata, entryLines []string) (string, error) {
	metadataBytes, err := marshalJSONNoHTMLEscape(metadata)
	if err != nil {
		return "", Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	var builder strings.Builder
	builder.Write(metadataBytes)
	builder.WriteByte('\n')
	for _, line := range entryLines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func commitImportedJSONL(repo *JSONLRepo, path, tmpPath string, metadata JSONLMetadata, entries []Entry) error {
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	storage, err := CreateJSONLStorage(tmpPath, metadata)
	if err != nil {
		return err
	}
	storage.metadata.Path = path
	if err := storage.ReplaceEntries(entries); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := OpenJSONLStorage(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return nil
}

type parsedJSONLTranscript struct {
	metadata     JSONLMetadata
	entries      []Entry
	entryLines   []string
	activeLeafID *string
}

func parseJSONLTranscript(data []byte) (parsedJSONLTranscript, error) {
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 || len(bytes.TrimSpace(lines[0])) == 0 {
		return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "session transcript is empty"}
	}
	var metadata JSONLMetadata
	if hasJSONLoneSurrogateEscape(lines[0]) {
		return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "parse session metadata: invalid Unicode escape"}
	}
	if err := json.Unmarshal(lines[0], &metadata); err != nil {
		return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "parse session metadata: " + err.Error()}
	}
	if strings.TrimSpace(metadata.ID) == "" {
		return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "session metadata is missing id"}
	}
	var entries []Entry
	var entryLines []string
	seen := map[string]bool{}
	var activeLeaf *string
	for index, line := range lines[1:] {
		line = bytes.TrimSuffix(line, []byte("\r"))
		trimmedLine := bytes.TrimSpace(line)
		if len(trimmedLine) == 0 {
			continue
		}
		var entry Entry
		if hasJSONLoneSurrogateEscape(trimmedLine) {
			return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "parse session entry line " + strconv.Itoa(index+2) + ": invalid Unicode escape"}
		}
		if err := json.Unmarshal(trimmedLine, &entry); err != nil {
			return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "parse session entry line " + strconv.Itoa(index+2) + ": " + err.Error()}
		}
		id := entry.ID()
		if seen[id] {
			return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "session transcript contains duplicate entry id"}
		}
		if entry.ParentID != nil && !seen[*entry.ParentID] {
			return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "session transcript contains dangling parent reference"}
		}
		if entry.Type() == EntryTypeLeaf {
			if entry.TargetID != nil && !seen[*entry.TargetID] {
				return parsedJSONLTranscript{}, Error{Code: ErrorCorrupted, Message: "session transcript contains dangling leaf target"}
			}
			activeLeaf = cloneStringPtr(entry.TargetID)
		} else {
			entryID := id
			activeLeaf = &entryID
		}
		seen[id] = true
		entries = append(entries, entry)
		entryLines = append(entryLines, string(line))
	}
	return parsedJSONLTranscript{metadata: metadata, entries: entries, entryLines: entryLines, activeLeafID: activeLeaf}, nil
}

func exportParsedSession(parsed parsedJSONLTranscript) ParsedSession {
	return ParsedSession{
		Metadata:           parsed.metadata,
		Entries:            append([]Entry(nil), parsed.entries...),
		OriginalEntryLines: append([]string(nil), parsed.entryLines...),
		ActiveLeafID:       cloneStringPtr(parsed.activeLeafID),
	}
}

func exportRewrittenSidecar(sidecar importedSidecar) RewrittenSidecar {
	return RewrittenSidecar{Bytes: append([]byte(nil), sidecar.bytes...), Count: sidecar.count, EnabledIDs: append([]string(nil), sidecar.enabledIDs...)}
}

func writeTarFile(writer *tar.Writer, name string, data []byte) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Format: tar.FormatGNU}); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if _, err := writer.Write(data); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return nil
}

func readTarArchive(path string) (map[string][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	defer file.Close()
	reader := tar.NewReader(file)
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
		}
		if header.Typeflag != tar.TypeReg {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive contains a non-file entry"}
		}
		if !utf8.ValidString(header.Name) {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive contains non-UTF-8 path"}
		}
		if !safeArchivePath(header.Name) {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive contains an unsafe path"}
		}
		limit, ok := archiveFileLimit(header.Name)
		if !ok {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive contains an unexpected file"}
		}
		content, err := io.ReadAll(io.LimitReader(reader, int64(limit+1)))
		if err != nil {
			return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
		}
		if len(content) > limit {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive file is too large"}
		}
		if _, exists := files[header.Name]; exists {
			return nil, Error{Code: ErrorCorrupted, Message: "session archive contains duplicate file paths"}
		}
		files[header.Name] = content
	}
	return files, nil
}

func archiveFileLimit(name string) (int, bool) {
	switch name {
	case archiveManifestPath:
		return maxArchiveManifestBytes, true
	case archiveSessionPath:
		return maxArchiveSessionBytes, true
	case archiveTriggersPath, archiveCronPath:
		return maxArchiveSidecarBytes, true
	default:
		return 0, false
	}
}

func safeArchivePath(name string) bool {
	if name == "" || filepath.IsAbs(name) {
		return false
	}
	slash := filepath.ToSlash(name)
	for _, part := range strings.Split(slash, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.ToSlash(filepath.Clean(slash)) == slash
}

func hasJSONLoneSurrogateEscape(data []byte) bool {
	inString := false
	for index := 0; index < len(data); index++ {
		if data[index] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[index] != '\\' || index+1 >= len(data) {
			continue
		}
		if data[index+1] != 'u' {
			index++
			continue
		}
		if index+5 >= len(data) {
			continue
		}
		value, ok := parseJSONHex4(data[index+2 : index+6])
		if !ok || value < 0xd800 || value > 0xdfff {
			index += 5
			continue
		}
		if value > 0xdbff {
			return true
		}
		if index+11 >= len(data) || data[index+6] != '\\' || data[index+7] != 'u' {
			return true
		}
		next, ok := parseJSONHex4(data[index+8 : index+12])
		if !ok || next < 0xdc00 || next > 0xdfff {
			return true
		}
		index += 11
	}
	return false
}

func parseJSONHex4(data []byte) (uint16, bool) {
	if len(data) != 4 {
		return 0, false
	}
	var value uint16
	for _, b := range data {
		var digit byte
		switch {
		case b >= '0' && b <= '9':
			digit = b - '0'
		case b >= 'a' && b <= 'f':
			digit = b - 'a' + 10
		case b >= 'A' && b <= 'F':
			digit = b - 'A' + 10
		default:
			return 0, false
		}
		value = value<<4 | uint16(digit)
	}
	return value, true
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sameStringPtr(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
