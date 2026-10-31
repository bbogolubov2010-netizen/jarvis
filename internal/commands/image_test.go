package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestCopyImageBodyAcceptsExactLimit(t *testing.T) {
	var dst bytes.Buffer
	written, err := copyImageBody(&dst, strings.NewReader("12345678"), 8)
	if err != nil {
		t.Fatalf("copyImageBody: %v", err)
	}
	if written != 8 || dst.String() != "12345678" {
		t.Fatalf("unexpected copy result: written=%d body=%q", written, dst.String())
	}
}

func TestCopyImageBodyRejectsOversizedResponse(t *testing.T) {
	var dst bytes.Buffer
	written, err := copyImageBody(&dst, strings.NewReader("123456789"), 8)
	if err == nil {
		t.Fatal("expected an error for an oversized response")
	}
	if written != 9 {
		t.Fatalf("written = %d, want 9", written)
	}
}
