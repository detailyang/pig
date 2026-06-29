package sessionarchive

import "github.com/detailyang/pig/session"

type ExportSummary = session.ExportSummary
type ExportArchiveOptions = session.ExportArchiveOptions
type ImportSummary = session.ImportSummary
type ImportArchiveOptions = session.ImportArchiveOptions
type AutomationActivationSummary = session.AutomationActivationSummary
type AutomationActivation = session.AutomationActivation
type ActivateTriggers = session.ActivateTriggers
type ParsedSession = session.ParsedSession
type RewrittenSidecar = session.RewrittenSidecar
type ArchiveManifestForImport = session.ArchiveManifestForImport

const AutomationActivationOff = session.AutomationActivationOff
const AutomationActivationAsk = session.AutomationActivationAsk
const AutomationActivationOn = session.AutomationActivationOn

const ActivateTriggersOff = session.ActivateTriggersOff
const ActivateTriggersAsk = session.ActivateTriggersAsk
const ActivateTriggersOn = session.ActivateTriggersOn

func DefaultExportPath(cwd, sessionID string) string {
	return session.DefaultExportPath(cwd, sessionID)
}

func ExportArchive(sessionPath, outputPath, pieVersion string) (ExportSummary, error) {
	return session.ExportArchive(sessionPath, outputPath, pieVersion)
}

func ExportSession(sessionPath, outputPath string, excludeTriggers bool, pieVersion string) (ExportSummary, error) {
	return session.ExportSession(sessionPath, outputPath, excludeTriggers, pieVersion)
}

func ExportArchiveWithOptions(sessionPath, outputPath, pieVersion string, options ExportArchiveOptions) (ExportSummary, error) {
	return session.ExportArchiveWithOptions(sessionPath, outputPath, pieVersion, options)
}

func ImportArchive(repo *session.JSONLRepo, archivePath, cwd string) (ImportSummary, error) {
	return session.ImportArchive(repo, archivePath, cwd)
}

func ImportSession(repo *session.JSONLRepo, archivePath, cwd string, activateTriggers ActivateTriggers) (ImportSummary, error) {
	return session.ImportSession(repo, archivePath, cwd, activateTriggers)
}

func ImportArchiveWithOptions(repo *session.JSONLRepo, archivePath, cwd string, options ImportArchiveOptions) (ImportSummary, error) {
	return session.ImportArchiveWithOptions(repo, archivePath, cwd, options)
}

func ActivateImported(sessionPath string, triggerIDs, cronIDs []string) (int, int, error) {
	return session.ActivateImported(sessionPath, triggerIDs, cronIDs)
}

func ParseSessionJSONL(text string) (ParsedSession, error) {
	return session.ParseSessionJSONL(text)
}

func RewriteTriggerSidecar(data []byte, activate bool) (RewrittenSidecar, error) {
	return session.RewriteTriggerSidecar(data, activate)
}

func RewriteCronSidecar(data []byte, activate bool) (RewrittenSidecar, error) {
	return session.RewriteCronSidecar(data, activate)
}

func RewriteSessionJSONL(parsed ParsedSession, manifest ArchiveManifestForImport, newID, cwd, path string) (string, error) {
	return session.RewriteSessionJSONL(parsed, manifest, newID, cwd, path)
}

func CommitImport(repo *session.JSONLRepo, sessionPath, tempPath, sessionContent string, sidecars map[string][]byte) error {
	return session.CommitImport(repo, sessionPath, tempPath, sessionContent, sidecars)
}
