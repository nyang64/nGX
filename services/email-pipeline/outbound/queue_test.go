package outbound

import (
	"testing"

	"agentmail/pkg/models"
)

func TestEmailAddrsToStrings_Basic(t *testing.T) {
	addrs := []models.EmailAddress{
		{Email: "a@example.com", Name: "Alice"},
		{Email: "b@example.com", Name: "Bob"},
	}
	got := emailAddrsToStrings(addrs)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0] != "a@example.com" || got[1] != "b@example.com" {
		t.Errorf("unexpected result: %v", got)
	}
}

func TestEmailAddrsToStrings_SkipsEmpty(t *testing.T) {
	addrs := []models.EmailAddress{
		{Email: "", Name: "No Email"},
		{Email: "a@example.com", Name: "Alice"},
	}
	got := emailAddrsToStrings(addrs)
	if len(got) != 1 || got[0] != "a@example.com" {
		t.Errorf("expected only non-empty emails, got %v", got)
	}
}

func TestEmailAddrsToStrings_Empty(t *testing.T) {
	got := emailAddrsToStrings(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
