package internal

import (
	"fmt"
	"strings"
)

// TopicReplacement represents a single old→new string replacement to apply to a topic.
type TopicReplacement struct {
	Old string
	New string
}

// TopicRewriteConfig holds the configuration for topic rewriting.
// Zero value is a no-op — no transformation is applied.
type TopicRewriteConfig struct {
	Replacements []TopicReplacement
	Prefix       string
}

// ParseTopicReplace parses a "old:new" string into a TopicReplacement.
// The split happens on the first colon, so "new" may contain colons.
// An empty "new" part (e.g. "legacy/:") means strip the matched string.
func ParseTopicReplace(s string) (TopicReplacement, error) {
	idx := strings.Index(s, ":")
	if idx == -1 {
		return TopicReplacement{}, fmt.Errorf("invalid topic replacement %q: missing ':' separator (expected old:new)", s)
	}
	old := s[:idx]
	if old == "" {
		return TopicReplacement{}, fmt.Errorf("invalid topic replacement %q: empty old value", s)
	}
	newVal := s[idx+1:]
	return TopicReplacement{Old: old, New: newVal}, nil
}

// TransformTopic applies replacements first, then prepends the prefix.
func TransformTopic(topic string, config TopicRewriteConfig) string {
	for _, r := range config.Replacements {
		topic = strings.Replace(topic, r.Old, r.New, 1)
	}
	if config.Prefix != "" {
		topic = config.Prefix + topic
	}
	return topic
}
