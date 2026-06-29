package triggers

import "time"

type Poller interface {
	Poll(now time.Time) []Trigger
}

type SupervisorOptions struct {
	Runtime *Runtime
	Pollers []Poller
}

type Supervisor struct {
	runtime *Runtime
	pollers []Poller
}

type PollResult struct {
	Trigger Trigger
	Outcome EvaluationOutcome
}

type SupervisorPollResult struct {
	Results    []PollResult
	Accepted   []PollResult
	Deduped    []PollResult
	Suppressed []PollResult
}

func NewSupervisor(options SupervisorOptions) *Supervisor {
	runtime := options.Runtime
	if runtime == nil {
		runtime = NewRuntime(DefaultRuntimeConfig())
	}
	return &Supervisor{runtime: runtime, pollers: append([]Poller(nil), options.Pollers...)}
}

func (supervisor *Supervisor) Runtime() *Runtime { return supervisor.runtime }

func (supervisor *Supervisor) Poll(now time.Time) SupervisorPollResult {
	var out SupervisorPollResult
	for _, poller := range supervisor.pollers {
		if poller == nil {
			continue
		}
		for _, trigger := range poller.Poll(now) {
			if trigger.ReceivedAt.IsZero() {
				trigger.ReceivedAt = now
			}
			outcome := supervisor.runtime.Evaluate(trigger)
			result := PollResult{Trigger: trigger, Outcome: outcome}
			out.Results = append(out.Results, result)
			switch outcome.Kind {
			case OutcomeAccept:
				out.Accepted = append(out.Accepted, result)
			case OutcomeDeduped:
				out.Deduped = append(out.Deduped, result)
			case OutcomeCycleSuppressed:
				out.Suppressed = append(out.Suppressed, result)
			}
		}
	}
	return out
}
