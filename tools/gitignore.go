package tools

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type gitignoreMatcher struct {
	rules []gitignoreRule
}

type gitignoreRule struct {
	base     string
	prefix   string
	pattern  string
	dirOnly  bool
	negate   bool
	anchored bool
}

var ignoreFileNames = []string{".ignore", ".gitignore"}

func loadGitignore(root string) (gitignoreMatcher, error) {
	var matcher gitignoreMatcher
	if absRoot, err := filepath.Abs(root); err == nil {
		var ancestors []string
		for dir := filepath.Dir(absRoot); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			ancestors = append(ancestors, dir)
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
		}
		for index := len(ancestors) - 1; index >= 0; index-- {
			ancestor := ancestors[index]
			prefix, err := filepath.Rel(ancestor, absRoot)
			if err != nil || prefix == "." || strings.HasPrefix(prefix, "..") {
				continue
			}
			for _, name := range ignoreFileNames {
				path := filepath.Join(ancestor, name)
				if _, err := os.Stat(path); err == nil {
					if err := matcher.loadFile(path, "", filepath.ToSlash(prefix)); err != nil {
						return matcher, err
					}
				}
			}
		}
	}
	if err := matcher.loadGitInfoExclude(root); err != nil {
		return matcher, err
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != root {
			return filepath.SkipDir
		}
		if entry.IsDir() || !isIgnoreFileName(entry.Name()) {
			return nil
		}
		base, _ := filepath.Rel(root, filepath.Dir(path))
		if base == "." {
			base = ""
		}
		return matcher.loadFile(path, filepath.ToSlash(base), "")
	})
	return matcher, err
}

func (matcher *gitignoreMatcher) loadGitInfoExclude(root string) error {
	path := filepath.Join(root, ".git", "info", "exclude")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return matcher.loadFile(path, "", "")
}

func isIgnoreFileName(name string) bool {
	for _, candidate := range ignoreFileNames {
		if name == candidate {
			return true
		}
	}
	return false
}

func (matcher *gitignoreMatcher) loadFile(path, base, prefix string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(line, "!")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		rule := gitignoreRule{base: base, prefix: prefix, pattern: strings.TrimSuffix(line, "/"), dirOnly: strings.HasSuffix(line, "/"), negate: negate, anchored: anchored}
		if rule.pattern != "" {
			matcher.rules = append(matcher.rules, rule)
		}
	}
	return scanner.Err()
}

func (matcher gitignoreMatcher) ignores(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	ignored := false
	for _, rule := range matcher.rules {
		if !rule.applies(rel) {
			continue
		}
		if rule.matches(rel, isDir) {
			ignored = !rule.negate
		}
	}
	return ignored
}

func (rule gitignoreRule) applies(rel string) bool {
	return rule.base == "" || rel == rule.base || strings.HasPrefix(rel, rule.base+"/")
}

func (rule gitignoreRule) matches(rel string, isDir bool) bool {
	local := rel
	if rule.base != "" {
		local = strings.TrimPrefix(strings.TrimPrefix(rel, rule.base), "/")
	}
	if rule.prefix != "" {
		local = rule.prefix + "/" + local
	}
	base := pathBase(local)
	if rule.dirOnly && !isDir && !strings.Contains(local, "/") {
		return false
	}
	return matchedIgnorePattern(rule.pattern, local, base, rule.anchored)
}

func matchedIgnorePattern(pattern, rel, base string, anchored bool) bool {
	if anchored || strings.Contains(pattern, "/") {
		return matchPathPattern(pattern, rel) || strings.HasPrefix(rel, pattern+"/")
	}
	matched, _ := filepath.Match(pattern, base)
	return matched || rel == pattern || strings.HasPrefix(rel, pattern+"/")
}

func matchPathPattern(pattern, rel string) bool {
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, rel)
		return matched
	}
	return matchPathSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchPathSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		if matchPathSegments(pattern[1:], path) {
			return true
		}
		return len(path) > 0 && matchPathSegments(pattern, path[1:])
	}
	if len(path) == 0 {
		return false
	}
	matched, _ := filepath.Match(pattern[0], path[0])
	return matched && matchPathSegments(pattern[1:], path[1:])
}

func pathBase(path string) string {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return path
	}
	return path[index+1:]
}
