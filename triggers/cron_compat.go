package triggers

func CronControlPlaneAudit(op string, actor string, before *CronJob, after *CronJob) map[string]any {
	out := map[string]any{"op": op, "actor": actor}
	if before != nil {
		out["before"] = *before
	}
	if after != nil {
		out["after"] = *after
	}
	return out
}

func cron_control_plane_audit(op string, actor string, before *CronJob, after *CronJob) map[string]any {
	return CronControlPlaneAudit(op, actor, before, after)
}
