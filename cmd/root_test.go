package cmd

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestParseBrokerURI(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantUsername string
		wantPassword string
		wantHost     string
	}{
		{
			name:         "simple credentials",
			input:        "tcp://user:pass@localhost:1883",
			wantUsername: "user",
			wantPassword: "pass",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with hash",
			input:        "tcp://user:#test@localhost:1883",
			wantUsername: "user",
			wantPassword: "#test",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with at sign",
			input:        "tcp://user:p@ss@localhost:1883",
			wantUsername: "user",
			wantPassword: "p@ss",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with colon",
			input:        "tcp://user:p:ss@localhost:1883",
			wantUsername: "user",
			wantPassword: "p:ss",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with multiple special chars",
			input:        "tcp://user:p@ss#w:rd@localhost:1883",
			wantUsername: "user",
			wantPassword: "p@ss#w:rd",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with percent-encoded space",
			input:        "tcp://user:pass%20word@localhost:1883",
			wantUsername: "user",
			wantPassword: "pass word",
			wantHost:     "localhost:1883",
		},
		{
			name:         "password with dollar and exclamation",
			input:        "tcp://user:p@s$w0rd!@host:1883",
			wantUsername: "user",
			wantPassword: "p@s$w0rd!",
			wantHost:     "host:1883",
		},
		{
			name:         "no auth",
			input:        "tcp://localhost:1883",
			wantUsername: "",
			wantPassword: "",
			wantHost:     "localhost:1883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseBrokerURI(tt.input)
			if err != nil {
				t.Fatalf("parseBrokerURI(%q) returned error: %v", tt.input, err)
			}

			var gotUser, gotPass string
			if u.User != nil {
				gotUser = u.User.Username()
				gotPass, _ = u.User.Password()
			}

			if gotUser != tt.wantUsername {
				t.Errorf("username = %q, want %q", gotUser, tt.wantUsername)
			}
			if gotPass != tt.wantPassword {
				t.Errorf("password = %q, want %q", gotPass, tt.wantPassword)
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}
		})
	}
}

func TestParseBrokerURI_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "invalid percent-encoded username",
			input:   "tcp://%ZZuser:pass@host:1883",
			wantErr: true,
		},
		{
			name:    "invalid percent-encoded password",
			input:   "tcp://user:%ZZpass@host:1883",
			wantErr: true,
		},
		{
			name:    "missing scheme",
			input:   "localhost:1883",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBrokerURI(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("parseBrokerURI(%q) expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("parseBrokerURI(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestParseBrokerURI_EdgeCases(t *testing.T) {
	longPass := strings.Repeat("a", 1000)
	tests := []struct {
		name     string
		input    string
		wantUser string
		wantPass string
		wantHost string
	}{
		{
			name:     "very long password",
			input:    "tcp://user:" + longPass + "@host:1883",
			wantUser: "user",
			wantPass: longPass,
			wantHost: "host:1883",
		},
		{
			name:     "empty password",
			input:    "tcp://user:@host:1883",
			wantUser: "user",
			wantPass: "",
			wantHost: "host:1883",
		},
		{
			name:     "empty username",
			input:    "tcp://:pass@host:1883",
			wantUser: "",
			wantPass: "pass",
			wantHost: "host:1883",
		},
		{
			name:     "only @ sign",
			input:    "tcp://@host:1883",
			wantUser: "",
			wantPass: "",
			wantHost: "host:1883",
		},
		{
			name:     "URI with path",
			input:    "tcp://user:pass@host:1883/path",
			wantUser: "user",
			wantPass: "pass",
			wantHost: "host:1883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseBrokerURI(tt.input)
			if err != nil {
				t.Fatalf("parseBrokerURI(%q) returned error: %v", tt.input, err)
			}

			var gotUser, gotPass string
			if u.User != nil {
				gotUser = u.User.Username()
				gotPass, _ = u.User.Password()
			}

			if gotUser != tt.wantUser {
				t.Errorf("username = %q, want %q", gotUser, tt.wantUser)
			}
			if gotPass != tt.wantPass {
				t.Errorf("password = %q, want %q", gotPass, tt.wantPass)
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}
		})
	}
}

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func TestConfigFile_LoadsAllKeys(t *testing.T) {
	v := viper.New()
	v.SetConfigFile(testdataPath("mirror.toml"))
	require.NoError(t, v.ReadInConfig())

	require.Equal(t, "tcp://testuser:testpass@src-host:1883", v.GetString("source"))
	require.Equal(t, "tcp://testuser:testpass@dst-host:1883", v.GetString("target"))
	require.Equal(t, true, v.GetBool("verbose"))
	require.Equal(t, []string{"test/#", "foo/bar"}, v.GetStringSlice("topic_filter"))
	require.Equal(t, "test-mirror", v.GetString("name"))
	require.Equal(t, 9999, v.GetInt("health_port"))
	require.Equal(t, false, v.GetBool("clean_session"))
	require.Equal(t, "prefix/", v.GetString("topic_prefix"))
	require.Equal(t, []string{"old:new", "alpha:beta"}, v.GetStringSlice("topic_replace"))
	require.Equal(t, "10s", v.GetString("publish_timeout"))
}

func TestConfigFlag_SetsConfigFile(t *testing.T) {
	oldCfg := cfgFile
	defer func() { cfgFile = oldCfg }()

	cfgFile = ""

	err := rootCmd.PersistentFlags().Set("config", "/tmp/test.toml")
	require.NoError(t, err)

	require.Equal(t, "/tmp/test.toml", cfgFile)
}

func TestIsValidUrl(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid tcp",
			input:   "tcp://localhost:1883",
			wantErr: false,
		},
		{
			name:    "valid with auth",
			input:   "tcp://user:pass@localhost:1883",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no host",
			input:   "tcp://",
			wantErr: true,
		},
		{
			name:    "garbage",
			input:   "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isValidUrl(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("isValidUrl(%q) expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("isValidUrl(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}
