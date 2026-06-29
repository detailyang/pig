package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type MemoryTool struct {
	Dir string
}

func NewMemoryTool(dir string) MemoryTool { return MemoryTool{Dir: dir} }

func (MemoryTool) Name() string { return "memory" }
func (MemoryTool) Description() string {
	return "Persistent cross-session memory. action=save (requires name/description/content/optional type), action=list, action=read (requires name), action=forget (requires name). Saved entries are auto-injected into the system prompt of future sessions."
}
func (MemoryTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"save", "list", "read", "forget"}, "description": "Operation to perform."}, "name": map[string]any{"type": "string", "description": "Short kebab-case slug (required for save/read/forget)."}, "description": map[string]any{"type": "string", "description": "One-line summary (save only)."}, "type": map[string]any{"type": "string", "description": "Memory category (e.g. user/feedback/project/reference). Default: user."}, "content": map[string]any{"type": "string", "description": "Body of the memory (save only)."}}, "required": []string{"action"}}
}
func (tool MemoryTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	action, err := memoryActionArg(call)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if err := os.MkdirAll(tool.Dir, 0o755); err != nil {
		return agent.ToolResult{}, fmt.Errorf("memory dir: %w", err)
	}
	switch action {
	case "save":
		return tool.save(call)
	case "list":
		return tool.list(call)
	case "read":
		return tool.read(call)
	case "forget":
		return tool.forget(call)
	default:
		return agent.ToolResult{}, fmt.Errorf("unknown action `%s`", action)
	}
}

func memoryActionArg(call ai.ToolCall) (string, error) {
	value, ok := call.Arguments["action"]
	if !ok {
		return "", fmt.Errorf("missing `action` (save | list | read | forget)")
	}
	action, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("missing `action` (save | list | read | forget)")
	}
	if !utf8.ValidString(action) {
		return "", fmt.Errorf("action must be valid UTF-8")
	}
	return action, nil
}

func (tool MemoryTool) save(call ai.ToolCall) (agent.ToolResult, error) {
	name, err := requiredStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(name) {
		return agent.ToolResult{}, fmt.Errorf("name must be valid UTF-8")
	}
	description, err := requiredStringArg(call, "description")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(description) {
		return agent.ToolResult{}, fmt.Errorf("description must be valid UTF-8")
	}
	content, err := requiredStringArg(call, "content")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(content) {
		return agent.ToolResult{}, fmt.Errorf("content must be valid UTF-8")
	}
	kind := optionalStringArgAllowEmpty(call, "type", "user")
	if !utf8.ValidString(kind) {
		return agent.ToolResult{}, fmt.Errorf("type must be valid UTF-8")
	}
	slug := SlugifyMemoryName(name)
	if slug == "" {
		return agent.ToolResult{}, fmt.Errorf("name slugifies to empty string")
	}
	path := filepath.Join(tool.Dir, slug+".md")
	payload := fmt.Sprintf("---\nname: %s\ndescription: %s\nmetadata:\n  type: %s\n---\n\n%s\n", slug, description, kind, content)
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		return agent.ToolResult{}, fmt.Errorf("write memory: %w", err)
	}
	if err := updateMemoryIndex(tool.Dir, slug, description); err != nil {
		return agent.ToolResult{}, err
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("Saved memory `%s` (%s)", slug, path), Details: map[string]any{"name": slug, "path": path}}, nil
}

func (tool MemoryTool) list(call ai.ToolCall) (agent.ToolResult, error) {
	entries, err := os.ReadDir(tool.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: "[no memories]", Details: map[string]any{"memories": []string{}}}, nil
		}
		return agent.ToolResult{}, fmt.Errorf("list memories: %w", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".md") && name != "MEMORY.md" {
			names = append(names, strings.TrimSuffix(name, ".md"))
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: "[no memories]", Details: map[string]any{"memories": []string{}}}, nil
	}
	var builder strings.Builder
	builder.WriteString("Memories:\n")
	for _, name := range names {
		builder.WriteString("  ")
		builder.WriteString(name)
		builder.WriteByte('\n')
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: builder.String(), Details: map[string]any{"memories": names}}, nil
}

func (tool MemoryTool) read(call ai.ToolCall) (agent.ToolResult, error) {
	name, err := requiredStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(name) {
		return agent.ToolResult{}, fmt.Errorf("name must be valid UTF-8")
	}
	path := filepath.Join(tool.Dir, SlugifyMemoryName(name)+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("read memory: %w", err)
	}
	if !utf8.Valid(data) {
		return agent.ToolResult{}, fmt.Errorf("read memory: stream did not contain valid UTF-8")
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: string(data), Details: map[string]any{"path": path}}, nil
}

func (tool MemoryTool) forget(call ai.ToolCall) (agent.ToolResult, error) {
	name, err := requiredStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(name) {
		return agent.ToolResult{}, fmt.Errorf("name must be valid UTF-8")
	}
	slug := SlugifyMemoryName(name)
	_ = os.Remove(filepath.Join(tool.Dir, slug+".md"))
	if err := removeMemoryIndexEntry(tool.Dir, slug); err != nil {
		return agent.ToolResult{}, err
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("Forgot memory `%s`.", slug), Details: map[string]any{"name": slug}}, nil
}

func SlugifyMemoryName(name string) string {
	var out strings.Builder
	prevHyphen := false
	for _, ch := range name {
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		isSep := unicode.IsSpace(ch) || ch == '-' || ch == '_'
		if isAlnum {
			out.WriteRune(ch)
			prevHyphen = false
		} else if isSep && !prevHyphen {
			out.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func updateMemoryIndex(dir, slug, description string) error {
	indexPath := filepath.Join(dir, "MEMORY.md")
	existing, _ := os.ReadFile(indexPath)
	if !utf8.Valid(existing) {
		existing = nil
	}
	line := fmt.Sprintf("- [%s](%s.md) — %s\n", slug, slug, description)
	var out strings.Builder
	replaced := false
	for _, existingLine := range memoryIndexLines(string(existing)) {
		if strings.HasPrefix(existingLine, "- ["+slug+"](") {
			out.WriteString(line)
			replaced = true
		} else {
			out.WriteString(existingLine)
			out.WriteByte('\n')
		}
	}
	if !replaced {
		out.WriteString(line)
	}
	return os.WriteFile(indexPath, []byte(out.String()), 0o644)
}

func removeMemoryIndexEntry(dir, slug string) error {
	indexPath := filepath.Join(dir, "MEMORY.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}
	if !utf8.Valid(data) {
		return nil
	}
	prefix := "- [" + slug + "]("
	var out strings.Builder
	for _, line := range memoryIndexLines(string(data)) {
		if strings.HasPrefix(line, prefix) {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if err := os.WriteFile(indexPath, []byte(out.String()), 0o644); err != nil {
		return fmt.Errorf("rewrite index: %w", err)
	}
	return nil
}

func memoryIndexLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func LoadMemoryBlock(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".md") && name != "MEMORY.md" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	var builder strings.Builder
	count := 0
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if !utf8.Valid(data) {
			continue
		}
		if count == 0 {
			builder.WriteString("<memory>\n")
			builder.WriteString("Persistent cross-session memory. These notes were saved in prior conversations and may be helpful. Use the `memory` tool with action=save to add more, action=forget to remove.\n\n")
		}
		builder.WriteString("--- ")
		builder.WriteString(name)
		builder.WriteString(" ---\n")
		builder.WriteString(strings.TrimSpace(string(data)))
		builder.WriteString("\n\n")
		count++
	}
	if count == 0 {
		return ""
	}
	builder.WriteString("</memory>")
	return builder.String()
}
