package cmd

import (
	"testing"
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
