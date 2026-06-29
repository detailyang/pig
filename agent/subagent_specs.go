package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SubagentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
}

func LoadSubagentSpecsDir(dir string) ([]SubagentSpec, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	var specs []SubagentSpec
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		spec, err := LoadSubagentSpecFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func LoadSubagentSpecFile(path string) (SubagentSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return SubagentSpec{}, err
	}
	defer file.Close()
	spec := SubagentSpec{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return SubagentSpec{}, fmt.Errorf("parse subagent spec %s: expected key = value", path)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			text, err := parseTomlString(value)
			if err != nil {
				return SubagentSpec{}, err
			}
			spec.Name = text
		case "description":
			text, err := parseTomlString(value)
			if err != nil {
				return SubagentSpec{}, err
			}
			spec.Description = text
		case "system_prompt":
			text, err := parseTomlString(value)
			if err != nil {
				return SubagentSpec{}, err
			}
			spec.SystemPrompt = text
		case "tools":
			tools, err := parseTomlStringArray(value)
			if err != nil {
				return SubagentSpec{}, err
			}
			spec.Tools = tools
		}
	}
	if err := scanner.Err(); err != nil {
		return SubagentSpec{}, err
	}
	if spec.Name == "" {
		return SubagentSpec{}, fmt.Errorf("subagent spec %s missing name", path)
	}
	return spec, nil
}

func parseTomlString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("expected quoted string")
	}
	return strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`), nil
}

func parseTomlStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, fmt.Errorf("expected string array")
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		text, err := parseTomlString(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, text)
	}
	return out, nil
}
