package router

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/brianaffirm/towr/internal/orchestrate"
)

// matchPolicy checks the prompt against policy rules. First match wins.
func matchPolicy(prompt string, rules []orchestrate.PolicyRule) (Decision, bool) {
	lowerPrompt := strings.ToLower(prompt)
	fileRefs := fileRefPattern.FindAllString(prompt, -1)

	for _, rule := range rules {
		pathMatch := rule.Path == ""
		keywordMatch := rule.Keyword == ""

		if rule.Path != "" {
			for _, ref := range fileRefs {
				if matched, _ := filepath.Match(rule.Path, ref); matched {
					pathMatch = true
					break
				}
				if globMatchDir(rule.Path, ref) {
					pathMatch = true
					break
				}
			}
		}

		if rule.Keyword != "" {
			if strings.Contains(lowerPrompt, strings.ToLower(rule.Keyword)) {
				keywordMatch = true
			}
		}

		if pathMatch && keywordMatch {
			tier := map[string]int{"haiku": 0, "sonnet": 1, "opus": 2}[rule.Model]
			reason := "policy"
			if rule.Path != "" {
				reason = fmt.Sprintf("policy:%s", rule.Path)
			} else if rule.Keyword != "" {
				reason = fmt.Sprintf("policy:keyword:%s", rule.Keyword)
			}
			return Decision{
				Model:           rule.Model,
				Reason:          reason,
				Tier:            tier,
				CanEscalate:     !rule.Pin,
				RequireApproval: rule.RequireApproval,
			}, true
		}
	}
	return Decision{}, false
}

func globMatchDir(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/")
	}
	return false
}
