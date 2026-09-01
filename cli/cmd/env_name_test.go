package cmd

import (
	"strings"
	"testing"
)

// Dots must stay legal. Banning them would have been the wrong fix for the
// viper key-splitting bug, and would reject environment names already in use.
func TestValidateEnvNameAcceptsRealNames(t *testing.T) {
	for _, name := range []string{
		"3.1",          // the range this whole investigation started from
		"dg-test-2.A",  // dot plus a trailing capital
		"dreadindex",   // plain
		"range_1",      // underscore
		"2",            // bare digit
		"a.b.c-d_e.99", // everything legal at once
	} {
		if err := validateEnvName(name); err != nil {
			t.Errorf("validateEnvName(%q) rejected a usable name: %v", name, err)
		}
	}
}

// The name becomes a directory and a filename, so traversal and separators
// have to be stopped before anything is written.
func TestValidateEnvNameRejectsPathEscapes(t *testing.T) {
	for _, name := range []string{"..", ".", "../evil", "a/b", `a\b`, "/abs"} {
		err := validateEnvName(name)
		if err == nil {
			t.Errorf("validateEnvName(%q) allowed a path escape", name)
			continue
		}
		if !strings.Contains(err.Error(), "escape") && !strings.Contains(err.Error(), "not usable") {
			t.Errorf("validateEnvName(%q) gave an unhelpful error: %v", name, err)
		}
	}
}

func TestValidateEnvNameRejectsUnusableNames(t *testing.T) {
	for _, name := range []string{
		"",           // empty
		"   ",        // whitespace only (callers TrimSpace first, but be safe)
		"a b",        // embedded space breaks argv and paths
		".hidden",    // leading dot creates a hidden directory
		"-leading",   // leading hyphen reads as a flag
		"has$dollar", // shell metacharacter
		"emoji🙂",     // non-ASCII in an Azure resource name
	} {
		if err := validateEnvName(name); err == nil {
			t.Errorf("validateEnvName(%q) allowed an unusable name", name)
		}
	}
}

// The rejection message has to say what IS allowed, or the user is left
// guessing at the rule.
func TestValidateEnvNameErrorIsActionable(t *testing.T) {
	err := validateEnvName("a b")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"letters", "3.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}
