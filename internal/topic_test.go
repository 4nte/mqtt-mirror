package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTopicReplace(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TopicReplacement
		wantErr bool
	}{
		{"simple replacement", "old:new", TopicReplacement{"old", "new"}, false},
		{"strip prefix", "legacy/:", TopicReplacement{"legacy/", ""}, false},
		{"colon in new value", "old:new:value", TopicReplacement{"old", "new:value"}, false},
		{"missing separator", "nocolon", TopicReplacement{}, true},
		{"empty old value", ":new", TopicReplacement{}, true},
		{"empty new value", "old:", TopicReplacement{"old", ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTopicReplace(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTransformTopic(t *testing.T) {
	tests := []struct {
		name   string
		topic  string
		config TopicRewriteConfig
		want   string
	}{
		{
			name:   "no-op with zero value config",
			topic:  "sensors/temp",
			config: TopicRewriteConfig{},
			want:   "sensors/temp",
		},
		{
			name:  "prefix only",
			topic: "sensors/temp",
			config: TopicRewriteConfig{
				Prefix: "site-a/",
			},
			want: "site-a/sensors/temp",
		},
		{
			name:  "replace only - strip prefix",
			topic: "legacy/sensors/temp",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{{Old: "legacy/", New: ""}},
			},
			want: "sensors/temp",
		},
		{
			name:  "replace only - swap substring",
			topic: "sensors/room1/temp",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{{Old: "room1", New: "room2"}},
			},
			want: "sensors/room2/temp",
		},
		{
			name:  "replace then prefix",
			topic: "legacy/sensors/temp",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{{Old: "legacy/", New: ""}},
				Prefix:       "site-a/",
			},
			want: "site-a/sensors/temp",
		},
		{
			name:  "multiple replacements",
			topic: "legacy/old/sensors/temp",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{
					{Old: "legacy/", New: ""},
					{Old: "old/", New: "new/"},
				},
			},
			want: "new/sensors/temp",
		},
		{
			name:  "no match - replacement does nothing",
			topic: "sensors/temp",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{{Old: "legacy/", New: ""}},
			},
			want: "sensors/temp",
		},
		{
			name:  "empty topic with prefix",
			topic: "",
			config: TopicRewriteConfig{
				Prefix: "prefix/",
			},
			want: "prefix/",
		},
		{
			name:  "replacement only applies once",
			topic: "a/a/b",
			config: TopicRewriteConfig{
				Replacements: []TopicReplacement{{Old: "a/", New: "x/"}},
			},
			want: "x/a/b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransformTopic(tt.topic, tt.config)
			require.Equal(t, tt.want, got)
		})
	}
}
