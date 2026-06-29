package triggers

import (
	"errors"
	"testing"
)

func TestCronUpstreamErrorConstructors(t *testing.T) {
	if AddCronJobErrorEmptyAction().Error() != "cron action cannot be empty" {
		t.Fatal("empty action error mismatch")
	}
	if AddCronJobErrorActionTooLarge(4096).Error() != "cron action exceeds 4096 bytes" {
		t.Fatal("action too large error mismatch")
	}
	if CronScheduleErrorWrongFieldCount().Error() != "cron schedule must have 5 fields: minute hour day-of-month month day-of-week" {
		t.Fatal("wrong field count error mismatch")
	}
	if CronScheduleErrorInvalidField("minute", "bad").Error() != "invalid cron field `minute`: bad" {
		t.Fatal("invalid field error mismatch")
	}
	if AddCronJobErrorSchedule(CronScheduleErrorWrongFieldCount()).Error() != CronScheduleErrorWrongFieldCount().Error() {
		t.Fatal("schedule wrapper mismatch")
	}
	if CronStorageErrorIo("disk").Error() != "cron storage io: disk" || CronStorageErrorParse("bad").Error() != "parse cron storage: bad" || CronStorageErrorSerialize("bad").Error() != "serialize cron storage: bad" {
		t.Fatal("cron storage error mismatch")
	}
}

func TestDynamicTriggerUpstreamErrorConstructors(t *testing.T) {
	if ParseTriggerRuleErrorEmpty().Error() != "usage: /new-trigger <when condition, run action>" {
		t.Fatal("empty parse error mismatch")
	}
	if ParseTriggerRuleErrorMissingAction().Error() != "could not split the trigger into a condition and action. In normal chat, ask pie to create the trigger so the model can extract them, or use `/new-trigger if condition, then action`." {
		t.Fatal("missing action error mismatch")
	}
	if ParseTriggerRuleErrorEmptyPart().Error() != "condition and action must both be non-empty" {
		t.Fatal("empty part error mismatch")
	}
	cause := errors.New("disk")
	if !errors.Is(DynamicTriggerStorageErrorRead(cause), cause) || !errors.Is(DynamicTriggerStorageErrorWrite(cause), cause) {
		t.Fatal("dynamic storage wrapping mismatch")
	}
	if AddTriggerRuleErrorParse(ParseTriggerRuleErrorEmpty()).Error() != ParseTriggerRuleErrorEmpty().Error() {
		t.Fatal("add trigger parse wrapper mismatch")
	}
}
