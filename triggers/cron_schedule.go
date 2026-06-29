package triggers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxScheduledCronActionBytes = 4096

const CronInboxTagsPerRun = 16

const LoopStateMaxChars = 2000

func LoopStatePath(cronSidecarPath string, jobID string) string {
	base := filepath.Base(cronSidecarPath)
	stem := strings.TrimSuffix(base, ".cron.toml")
	if stem == base {
		stem = "session"
	}
	short := jobID
	if len([]rune(short)) > 13 {
		short = string([]rune(short)[:13])
	}
	return filepath.Join(filepath.Dir(cronSidecarPath), stem+".loop-"+short+".md")
}

func ReadLoopState(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if !utf8.Valid(data) {
		return "", false
	}
	return capLoopState(string(data)), true
}

func WriteLoopState(path string, state string) error {
	return os.WriteFile(path, []byte(capLoopState(state)), 0o644)
}

func capLoopState(state string) string {
	trimmed := strings.TrimSpace(state)
	chars := []rune(trimmed)
	if len(chars) > LoopStateMaxChars {
		return string(chars[:LoopStateMaxChars]) + "…"
	}
	return trimmed
}

func ComposeStatefulCronPrompt(action string, state string) string {
	state = strings.TrimSpace(state)
	if state == "" {
		state = "(first run)"
	}
	return fmt.Sprintf("[loop-state] (your notes from the previous run of this recurring job)\n%s\n[/loop-state]\n\n%s\n\nOutput protocol (mandatory):\n- End your reply with <loop-state>notes for the next run</loop-state> — it REPLACES the saved state; keep it under 2000 characters and make it the information your next run needs (baselines, ids already seen, watermarks).\n- For each finding a human should act on, emit <inbox>one concise line</inbox>. No findings → no inbox tags; do not invent work.\n- Keep everything after the last tool call short so the tags are not truncated.", state, action)
}

func ComposeStatefulPrompt(action string, state string) string {
	return ComposeStatefulCronPrompt(action, state)
}

func ExtractCronTagBlock(text string, tag string) string {
	block, ok := FindCronTagBlock(text, tag)
	if !ok {
		return ""
	}
	return block
}

func ExtractTagBlock(text string, tag string) (string, bool) {
	return FindCronTagBlock(text, tag)
}

func FindCronTagBlock(text string, tag string) (string, bool) {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.LastIndex(text, open)
	if start < 0 {
		return "", false
	}
	rest := text[start+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:end]), true
}

func ExtractCronTagAll(text string, tag string, max int) []string {
	if max <= 0 {
		return nil
	}
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	var out []string
	rest := text
	for len(out) < max {
		start := strings.Index(rest, open)
		if start < 0 {
			break
		}
		after := rest[start+len(open):]
		end := strings.Index(after, close)
		if end < 0 {
			break
		}
		body := strings.TrimSpace(after[:end])
		if body != "" {
			out = append(out, body)
		}
		rest = after[end+len(close):]
	}
	return out
}

func ExtractTagAll(text string, tag string, max int) []string {
	return ExtractCronTagAll(text, tag, max)
}

func StripLoopProtocolTags(text string) string {
	out := text
	stripped := false
	for _, tag := range []string{"loop-state", "inbox"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		for {
			start := strings.Index(out, open)
			if start < 0 {
				break
			}
			rest := out[start:]
			end := strings.Index(rest, close)
			if end < 0 {
				break
			}
			out = out[:start] + out[start+end+len(close):]
			stripped = true
		}
	}
	if !stripped {
		return out
	}
	var builder strings.Builder
	previousBlank := false
	for _, line := range strings.Split(out, "\n") {
		trimmedRight := strings.TrimRight(line, " \t\r")
		blank := strings.TrimSpace(trimmedRight) == ""
		if blank && previousBlank {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(trimmedRight)
		previousBlank = blank
	}
	return strings.TrimSpace(builder.String())
}

type ScheduledCronJob struct {
	ID                  string
	Schedule            string
	Action              string
	Enabled             bool
	RunningTraceID      string
	runningTraceIDSet   bool
	LastDueAt           *time.Time
	LastFiredAt         *time.Time
	LastCompletedAt     *time.Time
	LastError           string
	lastErrorSet        bool
	SkippedOverlapCount uint64
	Stateful            bool
	CreatedAt           time.Time
}

type CronJob = ScheduledCronJob

type ScheduledDueJob struct {
	Job   ScheduledCronJob
	DueAt time.Time
}

func (job ScheduledCronJob) NextRunAfter(after time.Time) (time.Time, error) {
	return nextScheduledCronRun(job.Schedule, after)
}

func (job ScheduledCronJob) HasLastError() bool {
	return job.lastErrorSet
}

type ScheduledCronRegistry struct {
	mu          sync.Mutex
	jobs        []ScheduledCronJob
	storagePath string
	clock       func() time.Time
}

type CronRegistry = ScheduledCronRegistry

type AddCronJobError = error
type CronStorageError = error
type CronScheduleError = error

var globalScheduledCronRegistry = NewScheduledCronRegistry()

func NewScheduledCronRegistry() *ScheduledCronRegistry {
	return &ScheduledCronRegistry{clock: func() time.Time { return time.Now().UTC() }}
}

func NewCronRegistry() *CronRegistry { return NewScheduledCronRegistry() }

func GlobalCronRegistry() *CronRegistry { return globalScheduledCronRegistry }

func global_cron_registry() *CronRegistry { return GlobalCronRegistry() }

func (registry *ScheduledCronRegistry) LoadFromPath(path string) error {
	jobs, err := readScheduledCronJobs(path)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := validateScheduledCron(job.Schedule); err != nil {
			return err
		}
	}
	changed := clearStaleScheduledCronState(jobs)
	if changed {
		if err := writeScheduledCronJobs(path, jobs); err != nil {
			return err
		}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.jobs = jobs
	registry.storagePath = path
	if registry.clock == nil {
		registry.clock = func() time.Time { return time.Now().UTC() }
	}
	return nil
}

func (registry *ScheduledCronRegistry) List() []ScheduledCronJob {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return append([]ScheduledCronJob(nil), registry.jobs...)
}

func (registry *ScheduledCronRegistry) StoragePath() string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.storagePath
}

func (registry *ScheduledCronRegistry) ClearForTests() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.jobs = nil
	registry.storagePath = ""
	if registry.clock == nil {
		registry.clock = func() time.Time { return time.Now().UTC() }
	}
}

func (registry *ScheduledCronRegistry) AddJob(schedule, action string) (ScheduledCronJob, error) {
	return registry.AddJobFull(schedule, action, false)
}

func (registry *ScheduledCronRegistry) AddJobFull(schedule, action string, stateful bool) (ScheduledCronJob, error) {
	schedule = strings.TrimSpace(schedule)
	action = strings.TrimSpace(action)
	if action == "" {
		return ScheduledCronJob{}, fmt.Errorf("cron action cannot be empty")
	}
	if len(action) > maxScheduledCronActionBytes {
		return ScheduledCronJob{}, fmt.Errorf("cron action exceeds %d bytes", maxScheduledCronActionBytes)
	}
	if err := validateScheduledCron(schedule); err != nil {
		return ScheduledCronJob{}, err
	}
	job := ScheduledCronJob{ID: "cron-" + randomScheduledCronHex(16), Schedule: schedule, Action: action, Enabled: true, Stateful: stateful, CreatedAt: registry.now()}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append(append([]ScheduledCronJob(nil), registry.jobs...), job)
	if err := registry.saveLocked(next); err != nil {
		return ScheduledCronJob{}, err
	}
	registry.jobs = next
	return job, nil
}

func NormalizeScheduledCron(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if err := validateScheduledCron(trimmed); err == nil {
		return trimmed, nil
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "hourly", "every hour", "once an hour":
		return "0 * * * *", nil
	case "daily", "every day", "once a day":
		return "0 9 * * *", nil
	case "weekly", "every week", "once a week":
		return "0 9 * * 1", nil
	}
	if strings.Contains(trimmed, "每小时") || strings.Contains(trimmed, "每個小時") {
		return "0 * * * *", nil
	}
	if strings.Contains(trimmed, "每天") || strings.Contains(trimmed, "每日") {
		return "0 9 * * *", nil
	}
	if strings.Contains(trimmed, "每周") || strings.Contains(trimmed, "每週") {
		return "0 9 * * 1", nil
	}
	return "", fmt.Errorf("invalid schedule: provide a 5-field cron expression, or a supported alias such as hourly / every hour / 每小时")
}

func validateScheduledCron(schedule string) error {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("cron schedule must have 5 fields: minute hour day-of-month month day-of-week")
	}
	if _, err := parseScheduledCronField(fields[0], 0, 59); err != nil {
		return err
	}
	if _, err := parseScheduledCronField(fields[1], 0, 23); err != nil {
		return err
	}
	if _, err := parseScheduledCronField(fields[2], 1, 31); err != nil {
		return err
	}
	if _, err := parseScheduledCronField(fields[3], 1, 12); err != nil {
		return err
	}
	_, err := parseScheduledCronDayOfWeek(fields[4])
	return err
}

func (registry *ScheduledCronRegistry) SetJobEnabled(id string, enabled bool) (*ScheduledCronJob, error) {
	id = strings.TrimSpace(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]ScheduledCronJob(nil), registry.jobs...)
	for index := range next {
		if next[index].ID != id {
			continue
		}
		next[index].Enabled = enabled
		if !enabled {
			next[index].RunningTraceID = ""
		}
		updated := next[index]
		if err := registry.saveLocked(next); err != nil {
			return nil, err
		}
		registry.jobs = next
		return &updated, nil
	}
	return nil, nil
}

func (registry *ScheduledCronRegistry) RemoveJob(id string) (*ScheduledCronJob, error) {
	id = strings.TrimSpace(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]ScheduledCronJob(nil), registry.jobs...)
	for index, job := range next {
		if job.ID != id {
			continue
		}
		removed := job
		next = append(next[:index], next[index+1:]...)
		if err := registry.saveLocked(next); err != nil {
			return nil, err
		}
		registry.jobs = next
		return &removed, nil
	}
	return nil, nil
}

func (registry *ScheduledCronRegistry) DueJobs(since, now time.Time) []ScheduledDueJob {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]ScheduledCronJob(nil), registry.jobs...)
	var due []ScheduledDueJob
	changed := false
	for index := range next {
		job := &next[index]
		if !job.Enabled {
			continue
		}
		if err := validateScheduledCron(job.Schedule); err != nil {
			if job.LastError != "invalid schedule" {
				job.LastError = "invalid schedule"
				job.lastErrorSet = true
				changed = true
			}
			continue
		}
		dueAt, err := nextScheduledCronRun(job.Schedule, since)
		if err != nil {
			if job.LastError != "no next run within 5 years" {
				job.LastError = "no next run within 5 years"
				job.lastErrorSet = true
				changed = true
			}
			continue
		}
		if dueAt.After(now) {
			continue
		}
		if job.RunningTraceID != "" {
			job.SkippedOverlapCount = saturatingAddScheduledCronSkip(job.SkippedOverlapCount)
			job.LastDueAt = &dueAt
			job.LastError = "skipped: previous run still active"
			job.lastErrorSet = true
			changed = true
			continue
		}
		job.RunningTraceID = "cron-" + randomScheduledCronHex(16)
		job.LastDueAt = &dueAt
		firedAt := now.UTC()
		job.LastFiredAt = &firedAt
		job.LastError = ""
		job.lastErrorSet = false
		due = append(due, ScheduledDueJob{Job: *job, DueAt: dueAt})
		changed = true
	}
	if changed {
		_ = registry.saveLocked(next)
		registry.jobs = next
	}
	return due
}

func saturatingAddScheduledCronSkip(count uint64) uint64 {
	if count == math.MaxUint64 {
		return math.MaxUint64
	}
	return count + 1
}

func (registry *ScheduledCronRegistry) JobForTrace(traceID string) *ScheduledCronJob {
	if traceID == "" {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, job := range registry.jobs {
		if job.RunningTraceID == traceID {
			copyJob := job
			return &copyJob
		}
	}
	return nil
}

func (registry *ScheduledCronRegistry) MarkCompleted(traceID string, errorMessage string) {
	if traceID == "" {
		return
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]ScheduledCronJob(nil), registry.jobs...)
	for index := range next {
		if next[index].RunningTraceID != traceID {
			continue
		}
		next[index].RunningTraceID = ""
		completedAt := registry.now()
		next[index].LastCompletedAt = &completedAt
		next[index].LastError = errorMessage
		_ = registry.saveLocked(next)
		registry.jobs = next
		return
	}
}

func (registry *ScheduledCronRegistry) saveLocked(jobs []ScheduledCronJob) error {
	if registry.storagePath == "" {
		return nil
	}
	return writeScheduledCronJobs(registry.storagePath, jobs)
}

func (registry *ScheduledCronRegistry) now() time.Time {
	if registry.clock == nil {
		return time.Now().UTC()
	}
	return registry.clock().UTC()
}

func readScheduledCronJobs(path string) ([]ScheduledCronJob, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	return parseScheduledCronTOML(data)
}

func writeScheduledCronJobs(path string, jobs []ScheduledCronJob) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(formatScheduledCronTOML(jobs)), 0o644)
}

func clearStaleScheduledCronState(jobs []ScheduledCronJob) bool {
	changed := false
	for index := range jobs {
		if jobs[index].RunningTraceID == "" && !jobs[index].runningTraceIDSet {
			continue
		}
		jobs[index].RunningTraceID = ""
		jobs[index].runningTraceIDSet = false
		jobs[index].LastError = "cleared stale running state on startup"
		jobs[index].lastErrorSet = true
		changed = true
	}
	return changed
}

func parseScheduledCronTOML(data []byte) ([]ScheduledCronJob, error) {
	lines := strings.Split(string(data), "\n")
	var jobs []ScheduledCronJob
	current := -1
	seenID := map[int]bool{}
	seenSchedule := map[int]bool{}
	seenAction := map[int]bool{}
	seenEnabled := map[int]bool{}
	seenCreatedAt := map[int]bool{}
	seenJobs := false
	seenTopLevelFields := map[string]bool{}
	seenFields := map[int]map[string]bool{}
	skippingUnknownTable := false
	for _, raw := range lines {
		line := strings.TrimSpace(stripTOMLInlineComment(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isTOMLJobsArrayHeader(line) {
			skippingUnknownTable = false
			if seenJobs {
				return nil, fmt.Errorf("duplicate cron field: jobs")
			}
			jobs = append(jobs, ScheduledCronJob{})
			current = len(jobs) - 1
			seenFields[current] = map[string]bool{}
			continue
		}
		if isTOMLTableHeader(line) || isTOMLArrayTableHeader(line) {
			skippingUnknownTable = true
			current = -1
			continue
		}
		if skippingUnknownTable {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid cron line")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if current < 0 {
			if seenTopLevelFields[key] {
				return nil, fmt.Errorf("duplicate cron field: %s", key)
			}
			seenTopLevelFields[key] = true
			if key == "jobs" {
				seenJobs = true
				inlineJobs, err := parseInlineScheduledCronJobs(value)
				if err == nil {
					jobs = append(jobs, inlineJobs...)
					for index := range inlineJobs {
						jobIndex := len(jobs) - len(inlineJobs) + index
						seenID[jobIndex] = true
						seenSchedule[jobIndex] = true
						seenAction[jobIndex] = true
						seenEnabled[jobIndex] = true
						seenCreatedAt[jobIndex] = true
					}
					continue
				}
				return nil, err
			}
			continue
		}
		if seenFields[current][key] {
			return nil, fmt.Errorf("duplicate cron field: %s", key)
		}
		seenFields[current][key] = true
		job := &jobs[current]
		switch key {
		case "id":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, err
			}
			job.ID = parsed
			seenID[current] = true
		case "schedule":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, err
			}
			job.Schedule = parsed
			seenSchedule[current] = true
		case "action":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, err
			}
			job.Action = parsed
			seenAction[current] = true
		case "enabled":
			parsed, err := parseTOMLBool(value)
			if err != nil {
				return nil, err
			}
			job.Enabled = parsed
			seenEnabled[current] = true
		case "running_trace_id":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, err
			}
			job.RunningTraceID = parsed
			job.runningTraceIDSet = true
		case "last_due_at":
			parsed, err := parseTOMLTimePtr(value)
			if err != nil {
				return nil, err
			}
			job.LastDueAt = parsed
		case "last_fired_at":
			parsed, err := parseTOMLTimePtr(value)
			if err != nil {
				return nil, err
			}
			job.LastFiredAt = parsed
		case "last_completed_at":
			parsed, err := parseTOMLTimePtr(value)
			if err != nil {
				return nil, err
			}
			job.LastCompletedAt = parsed
		case "last_error":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return nil, err
			}
			job.LastError = parsed
			job.lastErrorSet = true
		case "skipped_overlap_count":
			count, err := parseTOMLUint(value)
			if err != nil {
				return nil, err
			}
			job.SkippedOverlapCount = count
		case "stateful":
			parsed, err := parseTOMLBool(value)
			if err != nil {
				return nil, err
			}
			job.Stateful = parsed
		case "created_at":
			parsed, err := parseTOMLTimePtr(value)
			if err != nil {
				return nil, err
			}
			if parsed != nil {
				job.CreatedAt = *parsed
				seenCreatedAt[current] = true
			}
		}
	}
	for index := range jobs {
		if !seenID[index] {
			return nil, fmt.Errorf("missing cron id")
		}
		if !seenSchedule[index] {
			return nil, fmt.Errorf("missing cron schedule")
		}
		if !seenAction[index] {
			return nil, fmt.Errorf("missing cron action")
		}
		if !seenEnabled[index] {
			return nil, fmt.Errorf("missing cron enabled")
		}
		if !seenCreatedAt[index] {
			return nil, fmt.Errorf("missing cron created_at")
		}
	}
	return jobs, nil
}

func parseInlineScheduledCronJobs(value string) ([]ScheduledCronJob, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("invalid cron jobs")
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, nil
	}
	var jobs []ScheduledCronJob
	for _, table := range splitTOMLInlineTables(inner) {
		table = strings.TrimSpace(table)
		if !strings.HasPrefix(table, "{") || !strings.HasSuffix(table, "}") {
			return nil, fmt.Errorf("invalid cron jobs")
		}
		job, err := parseInlineScheduledCronJob(strings.TrimSpace(table[1 : len(table)-1]))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func splitTOMLInlineTables(value string) []string {
	var tables []string
	start := 0
	depth := 0
	var stringQuote rune
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if stringQuote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' || char == '\'' {
			if stringQuote == 0 {
				stringQuote = char
			} else if stringQuote == char {
				stringQuote = 0
			}
			continue
		}
		if stringQuote != 0 {
			continue
		}
		switch char {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				tables = append(tables, value[start:index])
				start = index + 1
			}
		}
	}
	tables = append(tables, value[start:])
	return tables
}

func parseInlineScheduledCronJob(value string) (ScheduledCronJob, error) {
	job := ScheduledCronJob{}
	seenFields := map[string]bool{}
	for _, part := range splitTOMLInlineFields(value) {
		key, rawValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return ScheduledCronJob{}, fmt.Errorf("invalid cron jobs")
		}
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if seenFields[key] {
			return ScheduledCronJob{}, fmt.Errorf("duplicate cron field: %s", key)
		}
		seenFields[key] = true
		switch key {
		case "id":
			parsed, err := parseTOMLString(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.ID = parsed
		case "schedule":
			parsed, err := parseTOMLString(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.Schedule = parsed
		case "action":
			parsed, err := parseTOMLString(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.Action = parsed
		case "enabled":
			parsed, err := parseTOMLBool(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.Enabled = parsed
		case "running_trace_id":
			parsed, err := parseTOMLString(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.RunningTraceID = parsed
			job.runningTraceIDSet = true
		case "last_due_at":
			parsed, err := parseTOMLTimePtr(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.LastDueAt = parsed
		case "last_fired_at":
			parsed, err := parseTOMLTimePtr(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.LastFiredAt = parsed
		case "last_completed_at":
			parsed, err := parseTOMLTimePtr(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.LastCompletedAt = parsed
		case "last_error":
			parsed, err := parseTOMLString(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.LastError = parsed
			job.lastErrorSet = true
		case "skipped_overlap_count":
			count, err := parseTOMLUint(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.SkippedOverlapCount = count
		case "stateful":
			parsed, err := parseTOMLBool(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			job.Stateful = parsed
		case "created_at":
			parsed, err := parseTOMLTimePtr(rawValue)
			if err != nil {
				return ScheduledCronJob{}, err
			}
			if parsed != nil {
				job.CreatedAt = *parsed
			}
		}
	}
	if !seenFields["id"] {
		return ScheduledCronJob{}, fmt.Errorf("missing cron id")
	}
	if !seenFields["schedule"] {
		return ScheduledCronJob{}, fmt.Errorf("missing cron schedule")
	}
	if !seenFields["action"] {
		return ScheduledCronJob{}, fmt.Errorf("missing cron action")
	}
	if !seenFields["enabled"] {
		return ScheduledCronJob{}, fmt.Errorf("missing cron enabled")
	}
	if !seenFields["created_at"] {
		return ScheduledCronJob{}, fmt.Errorf("missing cron created_at")
	}
	return job, nil
}

func splitTOMLInlineFields(value string) []string {
	var fields []string
	start := 0
	var stringQuote rune
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if stringQuote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' || char == '\'' {
			if stringQuote == 0 {
				stringQuote = char
			} else if stringQuote == char {
				stringQuote = 0
			}
			continue
		}
		if stringQuote == 0 && char == ',' {
			fields = append(fields, value[start:index])
			start = index + 1
		}
	}
	fields = append(fields, value[start:])
	return fields
}

func isTOMLJobsArrayHeader(line string) bool {
	if !strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]]") {
		return false
	}
	return strings.TrimSpace(line[2:len(line)-2]) == "jobs"
}

func isTOMLTableHeader(line string) bool {
	if strings.HasPrefix(line, "[[") {
		return false
	}
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func isTOMLArrayTableHeader(line string) bool {
	return strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]")
}

func parseTOMLUint(value string) (uint64, error) {
	hasPlus := strings.HasPrefix(value, "+")
	value = strings.TrimPrefix(value, "+")
	if value == "" || strings.HasPrefix(value, "_") || strings.HasSuffix(value, "_") || strings.Contains(value, "__") {
		return 0, fmt.Errorf("invalid TOML integer")
	}
	base := 10
	if strings.HasPrefix(value, "0x") {
		if hasPlus {
			return 0, fmt.Errorf("invalid TOML integer")
		}
		base = 16
		value = value[2:]
	} else if strings.HasPrefix(value, "0o") {
		if hasPlus {
			return 0, fmt.Errorf("invalid TOML integer")
		}
		base = 8
		value = value[2:]
	} else if strings.HasPrefix(value, "0b") {
		if hasPlus {
			return 0, fmt.Errorf("invalid TOML integer")
		}
		base = 2
		value = value[2:]
	}
	if value == "" || strings.HasPrefix(value, "_") {
		return 0, fmt.Errorf("invalid TOML integer")
	}
	normalized := strings.ReplaceAll(value, "_", "")
	if base == 10 && len(normalized) > 1 && strings.HasPrefix(normalized, "0") {
		return 0, fmt.Errorf("invalid TOML integer")
	}
	return strconv.ParseUint(normalized, base, 64)
}

func stripTOMLInlineComment(line string) string {
	var stringQuote rune
	escaped := false
	for index, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if stringQuote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' || char == '\'' {
			if stringQuote == 0 {
				stringQuote = char
			} else if stringQuote == char {
				stringQuote = 0
			}
			continue
		}
		if stringQuote == 0 && char == '#' {
			return line[:index]
		}
	}
	return line
}

func parseTOMLBool(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid cron bool")
	}
}

func formatScheduledCronTOML(jobs []ScheduledCronJob) string {
	if len(jobs) == 0 {
		return "jobs = []\n"
	}
	var builder strings.Builder
	for index, job := range jobs {
		if index > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("[[jobs]]\n")
		writeTOMLString(&builder, "id", job.ID)
		writeTOMLString(&builder, "schedule", job.Schedule)
		writeTOMLString(&builder, "action", job.Action)
		writeTOMLBool(&builder, "enabled", job.Enabled)
		if job.RunningTraceID != "" {
			writeTOMLString(&builder, "running_trace_id", job.RunningTraceID)
		}
		if job.LastDueAt != nil {
			writeTOMLTime(&builder, "last_due_at", *job.LastDueAt)
		}
		if job.LastFiredAt != nil {
			writeTOMLTime(&builder, "last_fired_at", *job.LastFiredAt)
		}
		if job.LastCompletedAt != nil {
			writeTOMLTime(&builder, "last_completed_at", *job.LastCompletedAt)
		}
		if job.LastError != "" || job.lastErrorSet {
			writeTOMLString(&builder, "last_error", job.LastError)
		}
		if job.SkippedOverlapCount != 0 {
			fmt.Fprintf(&builder, "skipped_overlap_count = %d\n", job.SkippedOverlapCount)
		}
		writeTOMLBool(&builder, "stateful", job.Stateful)
		writeTOMLTime(&builder, "created_at", job.CreatedAt)
	}
	return builder.String()
}

func nextScheduledCronRun(schedule string, since time.Time) (time.Time, error) {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron schedule must have 5 fields: minute hour day-of-month month day-of-week")
	}
	minutes, err := parseScheduledCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, err
	}
	hours, err := parseScheduledCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, err
	}
	daysOfMonth, err := parseScheduledCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, err
	}
	months, err := parseScheduledCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, err
	}
	daysOfWeek, err := parseScheduledCronDayOfWeek(fields[4])
	if err != nil {
		return time.Time{}, err
	}
	start := since.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := start.AddDate(5, 0, 0)
	for candidate := start; !candidate.After(limit); candidate = candidate.Add(time.Minute) {
		local := candidate.In(time.Local)
		if minutes[local.Minute()] && hours[local.Hour()] && daysOfMonth[local.Day()] && months[int(local.Month())] && daysOfWeek[int(local.Weekday())] {
			return candidate, nil
		}
	}
	return time.Time{}, fmt.Errorf("no next run within 5 years")
}

func parseScheduledCronDayOfWeek(field string) (map[int]bool, error) {
	values, err := parseScheduledCronField(field, 0, 7)
	if err != nil {
		return nil, err
	}
	if values[7] {
		values[0] = true
		delete(values, 7)
	}
	return values, nil
}

func parseScheduledCronField(field string, min, max int) (map[int]bool, error) {
	values := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, invalidScheduledCronField(field, "empty item")
		}
		rangePart := part
		step := 1
		if before, after, ok := strings.Cut(part, "/"); ok {
			rangePart = before
			parsedStep, err := strconv.Atoi(after)
			if err != nil {
				return nil, invalidScheduledCronField(field, "step must be a positive integer")
			}
			if parsedStep <= 0 {
				return nil, invalidScheduledCronField(field, "step must be at least 1")
			}
			step = parsedStep
		}
		start, end, err := parseScheduledCronRange(field, rangePart, min, max)
		if err != nil {
			return nil, err
		}
		if start > end {
			return nil, invalidScheduledCronField(field, "range start must be <= range end")
		}
		for value := start; value <= end; value += step {
			values[value] = true
		}
	}
	return values, nil
}

func parseScheduledCronRange(field, raw string, min, max int) (int, int, error) {
	if raw == "*" {
		return min, max, nil
	}
	if start, end, ok := strings.Cut(raw, "-"); ok {
		parsedStart, err := parseScheduledCronNumber(field, start, min, max)
		if err != nil {
			return 0, 0, err
		}
		parsedEnd, err := parseScheduledCronNumber(field, end, min, max)
		if err != nil {
			return 0, 0, err
		}
		return parsedStart, parsedEnd, nil
	}
	value, err := parseScheduledCronNumber(field, raw, min, max)
	if err != nil {
		return 0, 0, err
	}
	return value, value, nil
}

func parseScheduledCronNumber(field, raw string, min, max int) (int, error) {
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0, invalidScheduledCronField(field, fmt.Sprintf("`%s` is not a number", raw))
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalidScheduledCronField(field, fmt.Sprintf("`%s` is not a number", raw))
	}
	if value < min || value > max {
		return 0, invalidScheduledCronField(field, fmt.Sprintf("value %d outside %d-%d", value, min, max))
	}
	return value, nil
}

func invalidScheduledCronField(field, reason string) error {
	return fmt.Errorf("invalid cron field `%s`: %s", field, reason)
}

func writeTOMLString(builder *strings.Builder, key, value string) {
	fmt.Fprintf(builder, "%s = \"%s\"\n", key, escapeTOMLBasicString(value))
}

func escapeTOMLBasicString(value string) string {
	var builder strings.Builder
	for _, char := range value {
		switch char {
		case '\b':
			builder.WriteString(`\b`)
		case '\t':
			builder.WriteString(`\t`)
		case '\n':
			builder.WriteString(`\n`)
		case '\f':
			builder.WriteString(`\f`)
		case '\r':
			builder.WriteString(`\r`)
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		default:
			if char < 0x20 || char == 0x7f {
				fmt.Fprintf(&builder, `\u%04X`, char)
			} else {
				builder.WriteRune(char)
			}
		}
	}
	return builder.String()
}

func writeTOMLBool(builder *strings.Builder, key string, value bool) {
	if value {
		fmt.Fprintf(builder, "%s = true\n", key)
		return
	}
	fmt.Fprintf(builder, "%s = false\n", key)
}

func writeTOMLTime(builder *strings.Builder, key string, value time.Time) {
	fmt.Fprintf(builder, "%s = %q\n", key, value.UTC().Format(time.RFC3339))
}

func parseTOMLString(value string) (string, error) {
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return value[1 : len(value)-1], nil
	}
	if err := validateTOMLBasicStringEscapes(value); err != nil {
		return "", err
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", err
	}
	return unquoted, nil
}

func validateTOMLBasicStringEscapes(value string) error {
	if len(value) < 2 || !strings.HasPrefix(value, "\"") || !strings.HasSuffix(value, "\"") {
		return nil
	}
	for index := 1; index < len(value)-1; index++ {
		if value[index] != '\\' {
			continue
		}
		index++
		if index >= len(value)-1 {
			return fmt.Errorf("invalid TOML string escape")
		}
		switch value[index] {
		case 'b', 't', 'n', 'f', 'r', '"', '\\', 'u', 'U':
			continue
		default:
			return fmt.Errorf("invalid TOML string escape")
		}
	}
	return nil
}

func parseTOMLTimePtr(value string) (*time.Time, error) {
	unquoted, err := parseTOMLString(value)
	if err != nil {
		unquoted = value
	}
	parsed, err := time.Parse(time.RFC3339, unquoted)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func randomScheduledCronHex(size int) string {
	bytes := make([]byte, size)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
