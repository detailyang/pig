package harness

import "github.com/detailyang/pig/triggers"

type TriggerSink = triggers.TriggerSink
type NotificationHook = triggers.NotificationHook
type DynNotificationHook = triggers.DynNotificationHook
type HookFuture = triggers.HookFuture
type HookError = triggers.HookError
type HookErrorKind = triggers.HookErrorKind
type HookState = triggers.HookState
type HookStateKind = triggers.HookStateKind
type Trigger = triggers.Trigger

const (
	HookErrorAuthFailed       = triggers.HookErrorAuthFailed
	HookErrorProtocolMismatch = triggers.HookErrorProtocolMismatch
	HookErrorDisconnected     = triggers.HookErrorDisconnected
	HookErrorSchemaInvalid    = triggers.HookErrorSchemaInvalid
	HookErrorSinkClosed       = triggers.HookErrorSinkClosed
	HookErrorOther            = triggers.HookErrorOther

	HookStateConnected    = triggers.HookStateConnected
	HookStateReconnecting = triggers.HookStateReconnecting
	HookStateDisconnected = triggers.HookStateDisconnected
	HookStateDisabled     = triggers.HookStateDisabled
	HookStateAuthFailed   = triggers.HookStateAuthFailed
)
