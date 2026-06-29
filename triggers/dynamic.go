package triggers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type DynamicRule struct {
	ID            string     `json:"id"`
	Condition     string     `json:"condition"`
	Action        string     `json:"action"`
	Enabled       bool       `json:"enabled"`
	FireOnce      bool       `json:"fire_once"`
	FiredAt       *time.Time `json:"fired_at"`
	PromoteToChat bool       `json:"promote_to_chat"`
	CreatedAt     time.Time  `json:"created_at"`
}

type DynamicTriggerRule = DynamicRule

type ParsedRule struct {
	Condition string
	Action    string
}

type ParsedTriggerRule = ParsedRule

type DynamicRegistry struct {
	mu          sync.Mutex
	rules       []DynamicRule
	storagePath string
}

type DynamicTriggerRegistry = DynamicRegistry

type ParseTriggerRuleError = error
type AddTriggerRuleError = error
type DynamicTriggerStorageError = error

var globalDynamicTriggerRegistry = NewDynamicRegistry()

func NewDynamicRegistry() *DynamicRegistry { return &DynamicRegistry{} }

func GlobalDynamicTriggerRegistry() *DynamicTriggerRegistry { return globalDynamicTriggerRegistry }

func GlobalRegistry() *DynamicTriggerRegistry { return GlobalDynamicTriggerRegistry() }

func global_registry() *DynamicTriggerRegistry { return GlobalDynamicTriggerRegistry() }

func (registry *DynamicRegistry) LoadFromPath(path string) error {
	rules, err := readRulesFile(path)
	if err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.rules = rules
	registry.storagePath = path
	return nil
}

func (registry *DynamicRegistry) StoragePath() string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.storagePath
}

func (registry *DynamicRegistry) ClearForTests() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.rules = nil
	registry.storagePath = ""
}

func (registry *DynamicRegistry) List() []DynamicRule {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return append([]DynamicRule(nil), registry.rules...)
}

func (registry *DynamicRegistry) AddRule(condition, action string) (DynamicRule, error) {
	return registry.AddRuleWithFlags(condition, action, true, false)
}

func (registry *DynamicRegistry) AddRuleWithOptions(condition, action string, fireOnce bool) (DynamicRule, error) {
	return registry.AddRuleWithFlags(condition, action, fireOnce, false)
}

func (registry *DynamicRegistry) AddRuleWithFlags(condition, action string, fireOnce bool, promoteToChat bool) (DynamicRule, error) {
	condition = strings.TrimSpace(condition)
	action = strings.TrimSpace(action)
	if condition == "" || action == "" {
		return DynamicRule{}, fmt.Errorf("condition and action must both be non-empty")
	}
	rule := DynamicRule{ID: "dyn-" + randomHex(16), Condition: condition, Action: action, Enabled: true, FireOnce: fireOnce, PromoteToChat: promoteToChat, CreatedAt: time.Now().UTC()}
	return registry.insertRule(rule)
}

func (registry *DynamicRegistry) AddFromSpec(spec string) (DynamicRule, error) {
	parsed, err := ParseTriggerRule(spec)
	if err != nil {
		return DynamicRule{}, err
	}
	return registry.AddRule(parsed.Condition, parsed.Action)
}

func (registry *DynamicRegistry) RemoveRule(id string) (*DynamicRule, error) {
	id = strings.TrimSpace(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]DynamicRule(nil), registry.rules...)
	for index, rule := range next {
		if rule.ID != id {
			continue
		}
		removed := rule
		next = append(next[:index], next[index+1:]...)
		if err := registry.persistLocked(next); err != nil {
			return nil, err
		}
		registry.rules = next
		return &removed, nil
	}
	return nil, nil
}

func (registry *DynamicRegistry) SetRuleEnabled(id string, enabled bool) (*DynamicRule, error) {
	id = strings.TrimSpace(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]DynamicRule(nil), registry.rules...)
	for index := range next {
		if next[index].ID != id {
			continue
		}
		next[index].Enabled = enabled
		if enabled {
			next[index].FiredAt = nil
		}
		if err := registry.persistLocked(next); err != nil {
			return nil, err
		}
		registry.rules = next
		updated := next[index]
		return &updated, nil
	}
	return nil, nil
}

func (registry *DynamicRegistry) ClearRules() (int, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	count := len(registry.rules)
	if count == 0 {
		return 0, nil
	}
	if err := registry.persistLocked(nil); err != nil {
		return 0, err
	}
	registry.rules = nil
	return count, nil
}

func (registry *DynamicRegistry) MarkRulesFired(ids []string) ([]DynamicRule, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append([]DynamicRule(nil), registry.rules...)
	now := time.Now().UTC()
	var changed []DynamicRule
	for index := range next {
		if !want[next[index].ID] || !next[index].FireOnce || !next[index].Enabled {
			continue
		}
		next[index].Enabled = false
		next[index].FiredAt = &now
		changed = append(changed, next[index])
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err := registry.persistLocked(next); err != nil {
		return nil, err
	}
	registry.rules = next
	return changed, nil
}

func (registry *DynamicRegistry) insertRule(rule DynamicRule) (DynamicRule, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	next := append(append([]DynamicRule(nil), registry.rules...), rule)
	if err := registry.persistLocked(next); err != nil {
		return DynamicRule{}, err
	}
	registry.rules = next
	return rule, nil
}

func (registry *DynamicRegistry) persistLocked(rules []DynamicRule) error {
	if registry.storagePath == "" {
		return nil
	}
	return writeRulesFile(registry.storagePath, rules)
}

func ParseTriggerRule(spec string) (ParsedRule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ParsedRule{}, fmt.Errorf("usage: /new-trigger <when condition, run action>")
	}
	markers := []string{"的时候，执行", "的时候,执行", "的时候 执行", "的时候执行", "的时候，", "的时候,", "时，执行", "时,执行", "时 执行", "时执行", "时，", "时,", "，则", ", 则", ",则", " 则 ", "则", "，就", ", 就", ",就", " 就 ", "，执行", ", 执行", ",执行", " 执行 ", " then ", " then run ", " then execute ", ", run ", ", execute ", ", do ", " run ", " execute "}
	lower := strings.ToLower(spec)
	for _, marker := range markers {
		haystack := spec
		if isASCII(marker) {
			haystack = lower
		}
		if index := strings.Index(haystack, marker); index >= 0 {
			condition := cleanCondition(spec[:index])
			action := cleanAction(spec[index+len(marker):])
			if condition == "" || action == "" {
				return ParsedRule{}, fmt.Errorf("condition and action must both be non-empty")
			}
			return ParsedRule{Condition: condition, Action: action}, nil
		}
	}
	return ParsedRule{}, fmt.Errorf("could not split the trigger into a condition and action. In normal chat, ask pie to create the trigger so the model can extract them, or use `/new-trigger if condition, then action`.")
}

func cleanCondition(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "当") {
		s = strings.TrimSpace(s[len("当"):])
	}
	if strings.HasPrefix(s, "如果") {
		s = strings.TrimSpace(s[len("如果"):])
	}
	if strings.HasPrefix(strings.ToLower(s), "when ") {
		s = strings.TrimSpace(s[len("when "):])
	} else if strings.HasPrefix(strings.ToLower(s), "if ") {
		s = strings.TrimSpace(s[len("if "):])
	}
	for strings.HasSuffix(s, "的时候") {
		s = strings.TrimSuffix(s, "的时候")
	}
	for strings.HasSuffix(s, "时") {
		s = strings.TrimSuffix(s, "时")
	}
	return strings.TrimSpace(s)
}

func cleanAction(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "执行") {
		s = strings.TrimSpace(s[len("执行"):])
	}
	if strings.HasPrefix(strings.ToLower(s), "run ") {
		s = strings.TrimSpace(s[len("run "):])
	} else if strings.HasPrefix(strings.ToLower(s), "execute ") {
		s = strings.TrimSpace(s[len("execute "):])
	}
	return s
}

func readRulesFile(path string) ([]DynamicRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dynamic triggers: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("read dynamic triggers: invalid UTF-8")
	}
	if err := rejectDuplicateJSONFields(data, map[string]bool{"version": true, "rules": true}); err != nil {
		return nil, fmt.Errorf("parse dynamic triggers: %w", err)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		return nil, fmt.Errorf("parse dynamic triggers: %w", err)
	}
	for _, field := range []string{"version", "rules"} {
		value, ok := topLevel[field]
		if !ok {
			return nil, fmt.Errorf("missing dynamic trigger file field %s", field)
		}
		if string(value) == "null" {
			return nil, fmt.Errorf("null dynamic trigger file field %s", field)
		}
	}
	var file struct {
		Version uint32            `json:"version"`
		Rules   []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse dynamic triggers: %w", err)
	}
	rules := make([]DynamicRule, 0, len(file.Rules))
	for _, raw := range file.Rules {
		if err := rejectDuplicateJSONFields(raw, map[string]bool{"id": true, "condition": true, "action": true, "enabled": true, "fire_once": true, "fired_at": true, "promote_to_chat": true, "created_at": true}); err != nil {
			return nil, fmt.Errorf("parse dynamic triggers: %w", err)
		}
		var rule DynamicRule
		if err := json.Unmarshal(raw, &rule); err != nil {
			return nil, fmt.Errorf("parse dynamic triggers: %w", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("parse dynamic triggers: %w", err)
		}
		for _, field := range []string{"id", "condition", "action", "enabled", "created_at"} {
			value, ok := fields[field]
			if !ok {
				return nil, fmt.Errorf("missing dynamic trigger field %s", field)
			}
			if string(value) == "null" {
				return nil, fmt.Errorf("null dynamic trigger field %s", field)
			}
		}
		if _, ok := fields["fire_once"]; !ok {
			rule.FireOnce = true
		}
		for _, field := range []string{"fire_once", "promote_to_chat"} {
			if value, ok := fields[field]; ok && string(value) == "null" {
				return nil, fmt.Errorf("null dynamic trigger field %s", field)
			}
		}
		rule.CreatedAt = rule.CreatedAt.UTC()
		if rule.FiredAt != nil {
			firedAt := rule.FiredAt.UTC()
			rule.FiredAt = &firedAt
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func writeRulesFile(path string, rules []DynamicRule) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write dynamic triggers: %w", err)
	}
	file := struct {
		Version uint32        `json:"version"`
		Rules   []DynamicRule `json:"rules"`
	}{Version: 1, Rules: rules}
	data, err := marshalJSONIndentNoHTMLEscape(file, "", "  ")
	if err != nil {
		return fmt.Errorf("write dynamic triggers: %w", err)
	}
	tmp := path + ".tmp-" + randomHex(6)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write dynamic triggers: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write dynamic triggers: %w", err)
	}
	return nil
}

func rejectDuplicateJSONFields(data []byte, known map[string]bool) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err == nil {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if token, err := decoder.Token(); err != nil {
		return err
	} else if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return err
		}
		field := fieldToken.(string)
		if known[field] {
			if _, ok := seen[field]; ok {
				return fmt.Errorf("duplicate field `%s`", field)
			}
			seen[field] = struct{}{}
		}
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func randomHex(bytesLen int) string {
	bytes := make([]byte, bytesLen)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func isASCII(value string) bool {
	for _, ch := range value {
		if ch > 127 {
			return false
		}
	}
	return true
}
