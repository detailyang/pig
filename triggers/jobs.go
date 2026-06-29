package triggers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JobKind string

const (
	JobKindCron JobKind = "cron"
	JobKindFile JobKind = "file"
)

type Job struct {
	ID        string        `json:"id"`
	Kind      JobKind       `json:"kind"`
	Label     string        `json:"label,omitempty"`
	Prompt    string        `json:"prompt,omitempty"`
	Every     time.Duration `json:"every,omitempty"`
	Path      string        `json:"path,omitempty"`
	Enabled   bool          `json:"enabled"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type JobSpec struct {
	ID      string
	Label   string
	Prompt  string
	Every   time.Duration
	Path    string
	Enabled bool
}

type JobRegistry struct {
	mu          sync.Mutex
	jobs        []Job
	storagePath string
	clock       func() time.Time
}

func NewJobRegistry() *JobRegistry {
	return &JobRegistry{clock: time.Now}
}

func (registry *JobRegistry) LoadFromPath(path string) error {
	jobs, err := readJobsFile(path)
	if err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.jobs = jobs
	registry.storagePath = path
	if registry.clock == nil {
		registry.clock = time.Now
	}
	return nil
}

func (registry *JobRegistry) StoragePath() string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.storagePath
}

func (registry *JobRegistry) List() []Job {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return append([]Job(nil), registry.jobs...)
}

func (registry *JobRegistry) AddCron(spec JobSpec) (Job, error) {
	if spec.Every <= 0 {
		return Job{}, fmt.Errorf("cron job every must be positive")
	}
	if spec.Prompt == "" {
		return Job{}, fmt.Errorf("cron job prompt is required")
	}
	return registry.upsert(Job{ID: spec.ID, Kind: JobKindCron, Label: spec.Label, Prompt: spec.Prompt, Every: spec.Every, Enabled: spec.Enabled})
}

func (registry *JobRegistry) AddFile(spec JobSpec) (Job, error) {
	if spec.Path == "" {
		return Job{}, fmt.Errorf("file job path is required")
	}
	return registry.upsert(Job{ID: spec.ID, Kind: JobKindFile, Label: spec.Label, Path: filepath.Clean(spec.Path), Enabled: spec.Enabled})
}

func (registry *JobRegistry) SetEnabled(id string, enabled bool) (Job, error) {
	if id == "" {
		return Job{}, fmt.Errorf("job id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for index := range registry.jobs {
		if registry.jobs[index].ID != id {
			continue
		}
		registry.jobs[index].Enabled = enabled
		registry.jobs[index].UpdatedAt = registry.now()
		if err := registry.saveLocked(); err != nil {
			return Job{}, err
		}
		return registry.jobs[index], nil
	}
	return Job{}, fmt.Errorf("no trigger job named %q", id)
}

func (registry *JobRegistry) Remove(id string) (Job, error) {
	if id == "" {
		return Job{}, fmt.Errorf("job id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for index, job := range registry.jobs {
		if job.ID != id {
			continue
		}
		registry.jobs = append(registry.jobs[:index], registry.jobs[index+1:]...)
		if err := registry.saveLocked(); err != nil {
			return Job{}, err
		}
		return job, nil
	}
	return Job{}, fmt.Errorf("no trigger job named %q", id)
}

func (registry *JobRegistry) Pollers() []Poller {
	registry.mu.Lock()
	jobs := append([]Job(nil), registry.jobs...)
	registry.mu.Unlock()
	var cronJobs []CronIntervalJob
	var fileWatches []FileWatch
	for _, job := range jobs {
		switch job.Kind {
		case JobKindCron:
			cronJobs = append(cronJobs, CronIntervalJob{ID: job.ID, Label: job.Label, Every: job.Every, Prompt: job.Prompt, Enabled: job.Enabled})
		case JobKindFile:
			fileWatches = append(fileWatches, FileWatch{ID: job.ID, Path: job.Path, Label: job.Label, Enabled: job.Enabled})
		}
	}
	var pollers []Poller
	if len(cronJobs) > 0 {
		pollers = append(pollers, NewCronIntervalAdapter(cronJobs))
	}
	if len(fileWatches) > 0 {
		pollers = append(pollers, NewFilePollAdapter(fileWatches))
	}
	return pollers
}

func (registry *JobRegistry) upsert(job Job) (Job, error) {
	if job.ID == "" {
		return Job{}, fmt.Errorf("job id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	now := registry.now()
	for index := range registry.jobs {
		if registry.jobs[index].ID != job.ID {
			continue
		}
		job.CreatedAt = registry.jobs[index].CreatedAt
		job.UpdatedAt = now
		registry.jobs[index] = job
		if err := registry.saveLocked(); err != nil {
			return Job{}, err
		}
		return job, nil
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	registry.jobs = append(registry.jobs, job)
	if err := registry.saveLocked(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (registry *JobRegistry) now() time.Time {
	if registry.clock == nil {
		return time.Now()
	}
	return registry.clock()
}

func (registry *JobRegistry) saveLocked() error {
	if registry.storagePath == "" {
		return nil
	}
	return writeJobsFile(registry.storagePath, registry.jobs)
}

func readJobsFile(path string) ([]Job, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs []Job
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("parse trigger jobs: %w", err)
	}
	return jobs, nil
}

func writeJobsFile(path string, jobs []Job) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := marshalJSONIndentNoHTMLEscape(jobs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
