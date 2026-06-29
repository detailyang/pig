package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type AutomationCounts struct {
	CronEnabled    int
	CronTotal      int
	TriggerEnabled int
	TriggerTotal   int
}

func (counts AutomationCounts) IsEmpty() bool {
	return counts.CronTotal == 0 && counts.TriggerTotal == 0
}

func (counts AutomationCounts) AnyEnabled() bool {
	return counts.CronEnabled > 0 || counts.TriggerEnabled > 0
}

func (counts AutomationCounts) Badge() string {
	if counts.IsEmpty() {
		return ""
	}
	var parts []string
	if counts.CronEnabled > 0 {
		parts = append(parts, fmt.Sprintf("%d cron", counts.CronEnabled))
	}
	if counts.TriggerEnabled > 0 {
		parts = append(parts, fmt.Sprintf("%d trigger", counts.TriggerEnabled))
	}
	if len(parts) == 0 {
		return "automation off"
	}
	return strings.Join(parts, ", ")
}

func AutomationCountsForSession(sessionPath string) AutomationCounts {
	counts := AutomationCounts{}
	if jobs, ok := readCronEnabledFlags(CronSidecarPath(sessionPath)); ok {
		counts.CronTotal = len(jobs)
		for _, enabled := range jobs {
			if enabled {
				counts.CronEnabled++
			}
		}
	}
	if rules, ok := readTriggerEnabledFlags(TriggerSidecarPath(sessionPath)); ok {
		counts.TriggerTotal = len(rules)
		for _, enabled := range rules {
			if enabled {
				counts.TriggerEnabled++
			}
		}
	}
	return counts
}

func automation_counts(sessionPath string) AutomationCounts {
	return AutomationCountsForSession(sessionPath)
}

func AutomationElsewhereHint(repo *JSONLRepo, current *string) string {
	paths, err := repo.List()
	if err != nil {
		return ""
	}
	currentStem := ""
	if current != nil {
		currentStem = strings.TrimSuffix(filepath.Base(*current), filepath.Ext(*current))
	}
	type holder struct {
		path   string
		counts AutomationCounts
	}
	var holders []holder
	for _, path := range paths {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if currentStem != "" && stem == currentStem {
			continue
		}
		counts := AutomationCountsForSession(path)
		if counts.AnyEnabled() {
			holders = append(holders, holder{path: path, counts: counts})
		}
	}
	if len(holders) == 0 {
		return ""
	}
	selected := holders[len(holders)-1]
	shortID := strings.TrimSuffix(filepath.Base(selected.path), filepath.Ext(selected.path))
	if len([]rune(shortID)) > 16 {
		shortID = string([]rune(shortID)[:16])
	}
	more := ""
	if extra := len(holders) - 1; extra > 0 {
		more = fmt.Sprintf(" (+%d more session(s))", extra)
	}
	return fmt.Sprintf("automation is session-scoped: session %s has %s enabled%s; resume it with `pie --resume-id %s`", shortID, selected.counts.Badge(), more, shortID)
}

func readTriggerEnabledFlags(path string) ([]bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if !utf8.Valid(data) {
		return nil, false
	}
	var file struct {
		Rules []struct {
			Enabled bool `json:"enabled"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, false
	}
	flags := make([]bool, 0, len(file.Rules))
	for _, rule := range file.Rules {
		flags = append(flags, rule.Enabled)
	}
	return flags, true
}

func readCronEnabledFlags(path string) ([]bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if !utf8.Valid(data) {
		return nil, false
	}
	text := string(data)
	if flags, ok := parseInlineCronJobs(text); ok {
		return flags, true
	}
	if strings.Contains(text, "[") && !strings.Contains(text, "[[jobs]]") {
		return nil, false
	}
	sections := strings.Split(text, "[[jobs]]")
	if len(sections) == 1 {
		return nil, true
	}
	flags := make([]bool, 0, len(sections)-1)
	for _, section := range sections[1:] {
		enabled, ok := parseTOMLEnabled(section)
		if !ok {
			return nil, false
		}
		flags = append(flags, enabled)
	}
	return flags, true
}

func parseInlineCronJobs(text string) ([]bool, bool) {
	start := strings.Index(text, "jobs")
	if start < 0 {
		return nil, false
	}
	_, value, ok := strings.Cut(text[start:], "=")
	if !ok {
		return nil, false
	}
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || strings.HasPrefix(value, "[[") {
		return nil, false
	}
	end := strings.LastIndex(value, "]")
	if end < 0 {
		return nil, false
	}
	value = value[1:end]
	flags := []bool{}
	for {
		open := strings.Index(value, "{")
		if open < 0 {
			break
		}
		close := strings.Index(value[open:], "}")
		if close < 0 {
			return nil, false
		}
		job := value[open+1 : open+close]
		enabled, ok := parseInlineEnabled(job)
		if !ok {
			return nil, false
		}
		flags = append(flags, enabled)
		value = value[open+close+1:]
	}
	if len(flags) == 0 && strings.TrimSpace(value) != "" {
		return nil, false
	}
	return flags, true
}

func parseInlineEnabled(job string) (bool, bool) {
	for _, part := range strings.Split(job, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "enabled" {
			continue
		}
		switch strings.TrimSpace(value) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	}
	return false, true
}

func parseTOMLEnabled(section string) (bool, bool) {
	for _, rawLine := range strings.Split(section, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "enabled" {
			continue
		}
		switch strings.TrimSpace(value) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	}
	return false, true
}
