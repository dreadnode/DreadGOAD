package cmd

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/lab"
)

func TestLabListLabelUsesDisplayNameWithoutChangingIdentifier(t *testing.T) {
	candidate := lab.Lab{Name: "GOAT", DisplayName: "GOAT"}
	if got := labListLabel(candidate); got != "GOAT" {
		t.Fatalf("labListLabel() = %q, want GOAT", got)
	}
	if candidate.Name != "GOAT" {
		t.Fatalf("internal identifier changed to %q", candidate.Name)
	}
}

func TestLabListLabelFallsBackToIdentifier(t *testing.T) {
	if got := labListLabel(lab.Lab{Name: "GOAD"}); got != "GOAD" {
		t.Fatalf("labListLabel() = %q, want GOAD", got)
	}
}
