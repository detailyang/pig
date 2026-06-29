package triggers

import "fmt"

func AddCronJobErrorEmptyAction() error { return fmt.Errorf("cron action cannot be empty") }

func AddCronJobErrorActionTooLarge(maxBytes int) error {
	return fmt.Errorf("cron action exceeds %d bytes", maxBytes)
}

func AddCronJobErrorSchedule(err error) error { return err }

func AddCronJobErrorStorage(err error) error { return err }

func CronStorageErrorIo(message string) error { return fmt.Errorf("cron storage io: %s", message) }

func CronStorageErrorParse(message string) error {
	return fmt.Errorf("parse cron storage: %s", message)
}

func CronStorageErrorSerialize(message string) error {
	return fmt.Errorf("serialize cron storage: %s", message)
}

func CronStorageErrorSchedule(err error) error { return err }

func CronScheduleErrorWrongFieldCount() error {
	return fmt.Errorf("cron schedule must have 5 fields: minute hour day-of-month month day-of-week")
}

func CronScheduleErrorInvalidField(field string, reason string) error {
	return fmt.Errorf("invalid cron field `%s`: %s", field, reason)
}

func ParseTriggerRuleErrorEmpty() error {
	return fmt.Errorf("usage: /new-trigger <when condition, run action>")
}

func ParseTriggerRuleErrorMissingAction() error {
	return fmt.Errorf("could not split the trigger into a condition and action. In normal chat, ask pie to create the trigger so the model can extract them, or use `/new-trigger if condition, then action`.")
}

func ParseTriggerRuleErrorEmptyPart() error {
	return fmt.Errorf("condition and action must both be non-empty")
}

func AddTriggerRuleErrorParse(err error) error { return err }

func AddTriggerRuleErrorStorage(err error) error { return err }

func DynamicTriggerStorageErrorRead(err error) error {
	return fmt.Errorf("read dynamic triggers: %w", err)
}

func DynamicTriggerStorageErrorParse(message string) error {
	return fmt.Errorf("parse dynamic triggers: %s", message)
}

func DynamicTriggerStorageErrorWrite(err error) error {
	return fmt.Errorf("write dynamic triggers: %w", err)
}
