package cmd

import (
	"strings"
	"testing"
)

// TestKaliCleanupScriptStripsAttackerHosts pins the /etc/hosts scrub behaviour:
// both modes inspect the file, apply mode rewrites it (root-guarded), and
// dry-run never mutates it. The strip filter must preserve the loopback block.
func TestKaliCleanupScriptStripsAttackerHosts(t *testing.T) {
	for _, apply := range []bool{false, true} {
		if !strings.Contains(buildKaliCleanupScript(apply), "/etc/hosts") {
			t.Fatalf("apply=%v: cleanup script does not reference /etc/hosts", apply)
		}
	}

	applyScript := buildKaliCleanupScript(true)
	if !strings.Contains(applyScript, "sudo -n cp") {
		t.Error("apply mode should rewrite /etc/hosts via `sudo -n cp` (fail-closed, no prompt)")
	}
	if !strings.Contains(applyScript, `$1 ~ /^127\./`) {
		t.Error("strip filter must preserve loopback (127.*) lines")
	}

	// Dry-run must be side-effect free: it may count, never rewrite.
	if strings.Contains(buildKaliCleanupScript(false), "sudo -n cp") {
		t.Error("dry-run must not contain the /etc/hosts rewrite command")
	}
}
