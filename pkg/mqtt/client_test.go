package mqtt

import (
	"strings"
	"testing"
)

func TestNewClient_NameTooLong(t *testing.T) {
	name := strings.Repeat("a", 11)
	_, err := NewClient("tcp://localhost:1883", "", "", false, name, true, nil, nil)
	if err == nil {
		t.Fatal("expected error for 11-char name, got nil")
	}
	if !strings.Contains(err.Error(), "maximum of 10 characters") {
		t.Errorf("expected error about max 10 characters, got: %v", err)
	}
}

func TestNewClient_NameBoundary(t *testing.T) {
	name := strings.Repeat("a", 10)
	// 10-char name should pass validation but fail on connect (no broker running)
	_, err := NewClient("tcp://localhost:19999", "", "", false, name, true, nil, nil)
	if err == nil {
		return // unlikely but acceptable
	}
	if strings.Contains(err.Error(), "maximum of 10 characters") {
		t.Errorf("10-char name should pass validation, but got name-length error: %v", err)
	}
}
