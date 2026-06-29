package triggers

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type CronIntervalJob struct {
	ID      string
	Label   string
	Every   time.Duration
	Prompt  string
	Enabled bool
}

type CronPayload struct {
	JobID  string        `json:"jobId"`
	Prompt string        `json:"prompt"`
	Every  time.Duration `json:"every"`
	DueAt  time.Time     `json:"dueAt"`
}

type CronIntervalAdapter struct {
	jobs    []CronIntervalJob
	lastDue map[string]time.Time
}

func NewCronIntervalAdapter(jobs []CronIntervalJob) *CronIntervalAdapter {
	return &CronIntervalAdapter{jobs: append([]CronIntervalJob(nil), jobs...), lastDue: map[string]time.Time{}}
}

func (adapter *CronIntervalAdapter) Poll(now time.Time) []Trigger {
	var out []Trigger
	for _, job := range adapter.jobs {
		if !job.Enabled || job.ID == "" || job.Every <= 0 {
			continue
		}
		last := adapter.lastDue[job.ID]
		if last.IsZero() {
			adapter.lastDue[job.ID] = now.Truncate(job.Every)
			continue
		}
		dueAt := last.Add(job.Every)
		if now.Before(dueAt) {
			continue
		}
		adapter.lastDue[job.ID] = dueAt
		label := job.Label
		if label == "" {
			label = job.ID
		}
		out = append(out, Trigger{
			Source:            Source{Kind: SourceLocal, Subkind: "cron"},
			SourceKind:        SourceKindLocal,
			SourceLabel:       label,
			EventLabel:        "cron due",
			PayloadVisibility: PayloadShared,
			Payload:           CronPayload{JobID: job.ID, Prompt: job.Prompt, Every: job.Every, DueAt: dueAt},
			IDempotencyKey:    fmt.Sprintf("cron:%s:%s", job.ID, dueAt.Format(time.RFC3339Nano)),
			ReplacementPolicy: ReplacementLatestReplaces,
			TraceID:           fmt.Sprintf("cron-%s-%d", job.ID, dueAt.UnixNano()),
			Authority:         Authority{PrincipalID: "local:cron", PrincipalLabel: "Local cron", CredentialScope: ScopeProject},
			ReceivedAt:        now,
		})
	}
	return out
}

type FileEvent string

const (
	FileEventCreated  FileEvent = "created"
	FileEventModified FileEvent = "modified"
	FileEventDeleted  FileEvent = "deleted"
)

type FileWatch struct {
	ID      string
	Path    string
	Label   string
	Enabled bool
}

type FilePayload struct {
	WatchID string    `json:"watchId"`
	Path    string    `json:"path"`
	Event   FileEvent `json:"event"`
	Size    int64     `json:"size,omitempty"`
	ModTime time.Time `json:"modTime,omitempty"`
}

type FilePollAdapter struct {
	watches []FileWatch
	seen    map[string]fileSnapshot
}

type fileSnapshot struct {
	exists  bool
	size    int64
	modTime time.Time
}

func NewFilePollAdapter(watches []FileWatch) *FilePollAdapter {
	return &FilePollAdapter{watches: append([]FileWatch(nil), watches...), seen: map[string]fileSnapshot{}}
}

func (adapter *FilePollAdapter) Poll(now time.Time) []Trigger {
	var out []Trigger
	for _, watch := range adapter.watches {
		if !watch.Enabled || watch.ID == "" || watch.Path == "" {
			continue
		}
		path := filepath.Clean(watch.Path)
		current := statFileSnapshot(path)
		previous, known := adapter.seen[watch.ID]
		adapter.seen[watch.ID] = current
		if !known {
			continue
		}
		event, changed := fileSnapshotEvent(previous, current)
		if !changed {
			continue
		}
		label := watch.Label
		if label == "" {
			label = watch.ID
		}
		out = append(out, Trigger{
			Source:            Source{Kind: SourceLocal, Subkind: "file"},
			SourceKind:        SourceKindLocal,
			SourceLabel:       label,
			EventLabel:        "file " + string(event),
			PayloadVisibility: PayloadShared,
			Payload:           FilePayload{WatchID: watch.ID, Path: path, Event: event, Size: current.size, ModTime: current.modTime},
			IDempotencyKey:    fmt.Sprintf("file:%s:%s:%d", watch.ID, event, now.UnixNano()),
			ReplacementPolicy: ReplacementLatestReplaces,
			TraceID:           fmt.Sprintf("file-%s-%d", watch.ID, now.UnixNano()),
			Authority:         Authority{PrincipalID: "local:file", PrincipalLabel: "Local file watch", CredentialScope: ScopeProject},
			ReceivedAt:        now,
		})
	}
	return out
}

func statFileSnapshot(path string) fileSnapshot {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fileSnapshot{}
	}
	return fileSnapshot{exists: true, size: info.Size(), modTime: info.ModTime()}
}

func fileSnapshotEvent(previous, current fileSnapshot) (FileEvent, bool) {
	switch {
	case !previous.exists && current.exists:
		return FileEventCreated, true
	case previous.exists && !current.exists:
		return FileEventDeleted, true
	case previous.exists && current.exists && (previous.size != current.size || !previous.modTime.Equal(current.modTime)):
		return FileEventModified, true
	default:
		return "", false
	}
}
