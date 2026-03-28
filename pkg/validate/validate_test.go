package validate

import (
	"errors"
	"strings"
	"testing"
)

type testInput struct {
	Name  string `validate:"required"`
	Email string `validate:"required,email"`
	Age   int    `validate:"min=0,max=150"`
}

// ---------------------------------------------------------------------------
// Struct
// ---------------------------------------------------------------------------

func TestStruct_Valid(t *testing.T) {
	s := testInput{Name: "Alice", Email: "alice@example.com", Age: 30}
	if err := Struct(s); err != nil {
		t.Errorf("expected nil error for valid struct, got: %v", err)
	}
}

func TestStruct_MissingRequiredField(t *testing.T) {
	s := testInput{Email: "alice@example.com", Age: 30} // Name is missing
	if err := Struct(s); err == nil {
		t.Error("expected error for missing required Name, got nil")
	}
}

func TestStruct_InvalidEmail(t *testing.T) {
	s := testInput{Name: "Bob", Email: "not-an-email", Age: 25}
	if err := Struct(s); err == nil {
		t.Error("expected error for invalid email, got nil")
	}
}

func TestStruct_AgeOutOfRange(t *testing.T) {
	s := testInput{Name: "Carol", Email: "carol@example.com", Age: 200}
	if err := Struct(s); err == nil {
		t.Error("expected error for age > 150, got nil")
	}
}

func TestStruct_NegativeAge(t *testing.T) {
	s := testInput{Name: "Dave", Email: "dave@example.com", Age: -1}
	if err := Struct(s); err == nil {
		t.Error("expected error for negative age, got nil")
	}
}

// ---------------------------------------------------------------------------
// ValidationErrors
// ---------------------------------------------------------------------------

func TestValidationErrors_LowercaseKeys(t *testing.T) {
	s := testInput{Email: "alice@example.com", Age: 30} // missing Name
	err := Struct(s)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	m := ValidationErrors(err)
	if _, ok := m["name"]; !ok {
		t.Errorf("expected lowercase key 'name' in map, got keys: %v", mapKeys(m))
	}
}

func TestValidationErrors_NonValidationError(t *testing.T) {
	plain := errors.New("something went wrong")
	m := ValidationErrors(plain)
	if _, ok := m["error"]; !ok {
		t.Errorf("expected 'error' key for non-ValidationErrors error, got keys: %v", mapKeys(m))
	}
	if m["error"] != "something went wrong" {
		t.Errorf("unexpected error message: %q", m["error"])
	}
}

func TestValidationErrors_MessageContainsTag(t *testing.T) {
	s := testInput{Name: "Eve", Email: "bad-email", Age: 30}
	err := Struct(s)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	m := ValidationErrors(err)
	msg, ok := m["email"]
	if !ok {
		t.Fatalf("expected 'email' key, got keys: %v", mapKeys(m))
	}
	if !strings.Contains(msg, "failed validation:") {
		t.Errorf("message %q does not contain 'failed validation:'", msg)
	}
	// The tag for an invalid email should be "email"
	if !strings.Contains(msg, "email") {
		t.Errorf("message %q does not contain the tag name 'email'", msg)
	}
}

func TestValidationErrors_RequiredTag(t *testing.T) {
	s := testInput{Email: "x@example.com", Age: 0} // Name missing
	err := Struct(s)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	m := ValidationErrors(err)
	msg, ok := m["name"]
	if !ok {
		t.Fatalf("expected 'name' key, got keys: %v", mapKeys(m))
	}
	if !strings.Contains(msg, "failed validation: required") {
		t.Errorf("message %q does not contain 'failed validation: required'", msg)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
