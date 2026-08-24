package common

import (
	"strings"
	"testing"
)

func TestNewAnthropicRequestIdShape(t *testing.T) {
	id := NewAnthropicRequestId()
	if !strings.HasPrefix(id, "req_011") {
		t.Fatalf("id = %q, want req_011 prefix", id)
	}
	if len(id) != len("req_011")+24 {
		t.Fatalf("id len = %d, want %d", len(id), len("req_011")+24)
	}
	if strings.HasPrefix(id, "2026") {
		t.Fatalf("public request-id must not use the date-prefixed internal format: %q", id)
	}
}
