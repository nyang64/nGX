package pagination

import (
	"testing"
)

// ---------------------------------------------------------------------------
// EncodeCursor / DecodeCursor
// ---------------------------------------------------------------------------

func TestEncodeCursor_SinglePartRoundTrip(t *testing.T) {
	encoded := EncodeCursor("abc123")
	parts, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if len(parts) != 1 || parts[0] != "abc123" {
		t.Errorf("round-trip got %v, want [abc123]", parts)
	}
}

func TestEncodeCursor_MultiplePartsJoinedWithPipe(t *testing.T) {
	encoded := EncodeCursor("part1", "part2", "part3")
	parts, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != "part1" || parts[1] != "part2" || parts[2] != "part3" {
		t.Errorf("parts mismatch: got %v", parts)
	}
}

func TestEncodeCursor_EmptyRoundTrip(t *testing.T) {
	// EncodeCursor() with no args encodes the empty string to base64("") == "".
	// DecodeCursor("") short-circuits and returns nil, nil — that is correct behaviour.
	encoded := EncodeCursor()
	parts, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts != nil {
		t.Errorf("expected nil parts for empty cursor, got %v", parts)
	}
}

// ---------------------------------------------------------------------------
// DecodeCursor
// ---------------------------------------------------------------------------

func TestDecodeCursor_EmptyStringReturnsNil(t *testing.T) {
	parts, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts != nil {
		t.Errorf("expected nil parts for empty cursor, got %v", parts)
	}
}

func TestDecodeCursor_InvalidBase64ReturnsError(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestDecodeCursor_SinglePart(t *testing.T) {
	encoded := EncodeCursor("hello")
	parts, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if len(parts) != 1 || parts[0] != "hello" {
		t.Errorf("expected [hello], got %v", parts)
	}
}

func TestDecodeCursor_TwoParts(t *testing.T) {
	encoded := EncodeCursor("foo", "bar")
	parts, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if len(parts) != 2 || parts[0] != "foo" || parts[1] != "bar" {
		t.Errorf("expected [foo bar], got %v", parts)
	}
}

// ---------------------------------------------------------------------------
// ClampLimit
// ---------------------------------------------------------------------------

func TestClampLimit(t *testing.T) {
	cases := []struct {
		input int
		want  int
	}{
		{0, 20},
		{-1, 20},
		{1, 1},
		{50, 50},
		{100, 100},
		{101, 100},
		{1000, 100},
	}
	for _, tc := range cases {
		got := ClampLimit(tc.input)
		if got != tc.want {
			t.Errorf("ClampLimit(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
