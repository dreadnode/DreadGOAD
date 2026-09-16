package cmd

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/lab"
)

func TestLabListLabelUsesDisplayNameWithoutChangingIdentifier(t *testing.T) {
	candidate := lab.Lab{Name: "SERVICE", DisplayName: "Example Service Range"}
	if got := labListLabel(candidate); got != "Example Service Range" {
		t.Fatalf("labListLabel() = %q, want display name", got)
	}
	if candidate.Name != "SERVICE" {
		t.Fatalf("internal identifier changed to %q", candidate.Name)
	}
}

func TestLabListLabelFallsBackToIdentifier(t *testing.T) {
	if got := labListLabel(lab.Lab{Name: "GOAD"}); got != "GOAD" {
		t.Fatalf("labListLabel() = %q, want GOAD", got)
	}
}
