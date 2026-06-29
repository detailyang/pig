package triggers

import (
	"context"
	"fmt"
	"time"

	"github.com/detailyang/pig/mcp"
)

type MCPNotificationHook struct {
	serverName    string
	notifications <-chan mcp.ServerNotification
	status        NotificationHookStatus
	ran           bool
}

type McpNotificationHook = MCPNotificationHook

func NewMCPNotificationHook(serverName string, notifications <-chan mcp.ServerNotification) *MCPNotificationHook {
	return &MCPNotificationHook{serverName: serverName, notifications: notifications, status: NotificationHookStatus{State: HookState{Kind: HookStateDisconnected, Reason: "not yet started"}, SubscriptionLabels: []string{"mcp:" + serverName}}}
}

func MCPNotificationHooksFromSources(sources []mcp.MCPNotificationSource) []NotificationHook {
	hooks := make([]NotificationHook, 0, len(sources))
	for _, source := range sources {
		hooks = append(hooks, NewMCPNotificationHook(source.ServerName, source.Notifications))
	}
	return hooks
}

func (hook *MCPNotificationHook) Label() string {
	if hook == nil {
		return "mcp"
	}
	return "mcp:" + hook.serverName
}

func (hook *MCPNotificationHook) Run(ctx context.Context, sink TriggerSink) error {
	if hook == nil {
		return nil
	}
	if hook.ran {
		return HookError{Kind: HookErrorOther, Reason: fmt.Sprintf("%s hook already ran; receiver consumed", hook.Label())}
	}
	hook.ran = true
	hook.status.State = HookState{Kind: HookStateConnected}
	hook.status.LastError = nil
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notification, ok := <-hook.notifications:
			if !ok {
				hook.status.State = HookState{Kind: HookStateDisconnected, Reason: "mcp transport closed"}
				return nil
			}
			trigger, mapped := MapMCPNotification(hook.serverName, notification, time.Now().UTC())
			if !mapped {
				hook.status.DroppedCount++
				message := fmt.Sprintf("dropped custom notification %q: missing `_meta.pie_dedup_key` or `_pie_dedup_key`", notification.Method)
				hook.status.LastError = &message
				continue
			}
			if err := sendMCPTrigger(ctx, sink, trigger); err != nil {
				if hookErr, ok := err.(HookError); ok && hookErr.Kind == HookErrorSinkClosed {
					hook.status.State = HookState{Kind: HookStateDisconnected, Reason: "sink closed"}
				}
				return err
			}
			now := time.Now().UTC()
			hook.status.LastEventAt = &now
			hook.status.LastError = nil
		}
	}
}

func sendMCPTrigger(ctx context.Context, sink TriggerSink, trigger Trigger) (err error) {
	defer func() {
		if recover() != nil {
			err = HookError{Kind: HookErrorSinkClosed}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case sink <- trigger:
		return nil
	}
}

func (hook *MCPNotificationHook) Status() NotificationHookStatus {
	if hook == nil {
		return PendingNotificationHookStatus()
	}
	status := hook.status
	if status.SubscriptionLabels == nil {
		status.SubscriptionLabels = []string{"mcp:" + hook.serverName}
	}
	return status
}
