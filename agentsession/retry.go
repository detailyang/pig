package agentsession

import (
	"regexp"
)

type ModelRef struct {
	Provider string
	ModelID  string
}

type RetrySettings struct {
	Enabled       bool
	MaxRetries    uint32
	BaseDelayMS   uint64
	MaxDelayMS    uint64
	FallbackModel *ModelRef
}

type RetryAction string

const (
	RetryActionRetry    RetryAction = "retry"
	RetryActionFallback RetryAction = "fallback"
	RetryActionGiveUp   RetryAction = "give_up"
)

type RetryDecision struct {
	Action       RetryAction
	Attempt      uint32
	DelayMS      uint64
	Fallback     *ModelRef
	ErrorMessage string
}

type RetryPolicy struct {
	settings     RetrySettings
	attempt      uint32
	fallbackUsed bool
}

func DefaultRetrySettings() RetrySettings {
	return RetrySettings{Enabled: true, MaxRetries: 5, BaseDelayMS: 1000, MaxDelayMS: 60000}
}

func Default() RetrySettings {
	return DefaultRetrySettings()
}

func NewRetryPolicy(settings RetrySettings) *RetryPolicy {
	if isZeroRetrySettings(settings) {
		settings = DefaultRetrySettings()
	}
	if settings.BaseDelayMS == 0 {
		settings.BaseDelayMS = 1000
	}
	if settings.MaxDelayMS == 0 {
		settings.MaxDelayMS = 60000
	}
	return &RetryPolicy{settings: settings}
}

func isZeroRetrySettings(settings RetrySettings) bool {
	return !settings.Enabled && settings.MaxRetries == 0 && settings.BaseDelayMS == 0 && settings.MaxDelayMS == 0 && settings.FallbackModel == nil
}

func (policy *RetryPolicy) Next(errorMessage string) RetryDecision {
	if policy == nil || !policy.settings.Enabled || !IsRetryableError(errorMessage) {
		return RetryDecision{Action: RetryActionGiveUp, ErrorMessage: errorMessage}
	}
	if policy.attempt >= policy.settings.MaxRetries {
		if policy.settings.FallbackModel != nil && !policy.fallbackUsed {
			policy.fallbackUsed = true
			policy.attempt = 0
			fallback := *policy.settings.FallbackModel
			return RetryDecision{Action: RetryActionFallback, Fallback: &fallback, Attempt: 0, ErrorMessage: errorMessage}
		}
		return RetryDecision{Action: RetryActionGiveUp, ErrorMessage: errorMessage}
	}
	policy.attempt++
	return RetryDecision{Action: RetryActionRetry, Attempt: policy.attempt, DelayMS: BackoffMS(policy.attempt, policy.settings.BaseDelayMS, policy.settings.MaxDelayMS), ErrorMessage: errorMessage}
}

func IsRetryableError(errorMessage string) bool {
	return retryablePattern.MatchString(errorMessage)
}

func BackoffMS(attempt uint32, base uint64, max uint64) uint64 {
	exponent := attempt
	if exponent > 0 {
		exponent--
	}
	if exponent > 10 {
		exponent = 10
	}
	value := uint64(0)
	if exponent >= 63 {
		value = max
	} else if base > 0 && base > ^uint64(0)>>exponent {
		value = max
	} else {
		value = base << exponent
	}
	if value > max {
		return max
	}
	return value
}

var retryablePattern = regexp.MustCompile(`(?i)overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|connection.?lost|websocket.?closed|websocket.?error|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|ended without|stream ended before message_stop|http2 request did not get a response|timed? out|timeout|terminated|retry delay`)
