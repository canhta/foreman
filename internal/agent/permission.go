package agent

import (
	"path/filepath"
	"strings"
)

type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

type Rule struct {
	Permission string
	Pattern    string
	Action     Action
}

type Ruleset []Rule

// editTools maps tool names that modify files to the "edit" permission.
var editTools = map[string]string{
	"write":      "edit",
	"edit":       "edit",
	"multiedit":  "edit",
	"applypatch": "edit",
}

// Evaluate checks a permission request against a ruleset.
// Returns the action from the last matching rule, or ActionDeny if no match.
func Evaluate(permission, pattern string, rules Ruleset) Action {
	normalizedPerm := strings.ToLower(permission)
	if mapped, ok := editTools[normalizedPerm]; ok {
		normalizedPerm = mapped
	}

	result := ActionDeny
	for _, rule := range rules {
		if matchPermission(normalizedPerm, strings.ToLower(rule.Permission)) &&
			matchPattern(pattern, rule.Pattern) {
			result = rule.Action
		}
	}
	return result
}

// Merge combines multiple rulesets into one (order preserved, last wins).
func Merge(rulesets ...Ruleset) Ruleset {
	var result Ruleset
	for _, rs := range rulesets {
		result = append(result, rs...)
	}
	return result
}

func matchPermission(perm, rule string) bool {
	if rule == "*" {
		return true
	}
	return perm == rule
}

func matchPattern(path, pattern string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	if matched {
		return true
	}
	matched, _ = filepath.Match(pattern, path)
	return matched
}
