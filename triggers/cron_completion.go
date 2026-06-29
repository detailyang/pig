package triggers

import "strings"

type StatefulCronCompletionOptions struct {
	CronSidecarPath string
	InboxPath       string
	TraceID         string
	Summary         string
	SessionID       string
	Error           string
}

type StatefulCronCompletionResult struct {
	Job          *ScheduledCronJob
	LoopState    string
	InboxEntries []InboxEntry
}

func HandleStatefulCronCompletion(registry *ScheduledCronRegistry, options StatefulCronCompletionOptions) (StatefulCronCompletionResult, error) {
	job := registry.JobForTrace(options.TraceID)
	if job == nil {
		return StatefulCronCompletionResult{}, nil
	}
	result := StatefulCronCompletionResult{Job: job}
	if options.Error != "" {
		registry.MarkCompleted(options.TraceID, options.Error)
		return result, nil
	}
	registry.MarkCompleted(options.TraceID, "")
	if !job.Stateful {
		return result, nil
	}
	loopState, hasLoopState := FindCronTagBlock(options.Summary, "loop-state")
	if hasLoopState {
		if err := WriteLoopState(LoopStatePath(options.CronSidecarPath, job.ID), loopState); err == nil {
			result.LoopState = loopState
		}
	}
	source := CronInboxSource(job.ID)
	for _, finding := range ExtractCronTagAll(options.Summary, "inbox", CronInboxTagsPerRun) {
		entry, err := AppendInbox(options.InboxPath, source, finding, options.TraceID, CronSessionStem(options.CronSidecarPath))
		if err != nil {
			continue
		}
		result.InboxEntries = append(result.InboxEntries, entry)
	}
	return result, nil
}

func CronInboxSource(jobID string) string {
	short := []rune(jobID)
	if len(short) > 13 {
		short = short[:13]
	}
	return "cron:" + string(short)
}

func CronSessionStem(cronSidecarPath string) string {
	base := strings.TrimSuffix(cronSidecarPath, ".cron.toml")
	if slash := strings.LastIndexAny(base, `/\\`); slash >= 0 {
		return base[slash+1:]
	}
	return base
}
