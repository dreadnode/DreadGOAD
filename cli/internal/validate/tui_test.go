package validate

import (
	"strings"
	"testing"
	"time"
)

func TestLiveModel_ApplyAndRender(t *testing.T) {
	m := newValidateModel(TUIConfig{Env: "prod-east", Region: "us-west-1"}, Report{}, nil, nil)
	for _, r := range []Result{
		{Status: "PASS", Category: "ACL", Name: "x"},
		{Status: "PASS", Category: "ACL", Name: "y"},
		{Status: "FAIL", Category: "MSSQL", Name: "sysadmin"},
		{Status: "WARN", Category: "Kerberos", Name: "asrep"},
	} {
		m.applyResult(r)
	}

	if m.report.Passed != 2 || m.report.Failed != 1 || m.report.Warnings != 1 {
		t.Fatalf("counters wrong: %+v", m.report)
	}
	if c := m.cats["MSSQL"]; c == nil || c.fail != 1 || c.total != 1 {
		t.Errorf("MSSQL cat wrong: %+v", c)
	}

	m.width = 120
	out := m.View()
	for _, want := range []string{"DreadGOAD VALIDATION", "ACL", "MSSQL", "Kerberos", "RUNNING", "prod-east", "us-west-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q\n%s", want, out)
		}
	}

	m.applyPhase(phaseEvent{kind: phaseDone})
	out2 := m.View()
	if !strings.Contains(out2, "COMPLETE") {
		t.Errorf("done View() missing COMPLETE\n%s", out2)
	}
	if !strings.Contains(out2, "success rate:") {
		t.Errorf("done View() missing success rate\n%s", out2)
	}
	if t.Failed() || testing.Verbose() {
		t.Logf("\n%s", out2)
	}
}

func TestLiveModel_PollPhases(t *testing.T) {
	m := newValidateModel(
		TUIConfig{Env: "staging", Region: "us-west-1", PollInterval: time.Minute},
		Report{Results: []Result{{Status: "PASS", Category: "Discovery", Name: "Found DC01"}}},
		nil, nil,
	)
	m.width = 120

	if m.report.Passed != 1 || m.cats["Discovery"] == nil {
		t.Fatalf("seed not applied: %+v", m.report)
	}

	// Iteration 1 starts running; seed retained.
	m.applyPhase(phaseEvent{kind: phaseRunning, iteration: time.Now()})
	m.applyResult(Result{Status: "PASS", Category: "ACL", Name: "x"})
	if m.report.Passed != 2 {
		t.Fatalf("first iteration count wrong: %+v", m.report)
	}

	// Transition to waiting.
	until := time.Now().Add(20 * time.Second)
	m.applyPhase(phaseEvent{kind: phaseWaiting, deadline: until})
	if m.phase != phaseWaiting {
		t.Errorf("phase not waiting: %v", m.phase)
	}
	if !strings.Contains(m.View(), "WAITING") {
		t.Errorf("View missing WAITING\n%s", m.View())
	}
	if !strings.Contains(m.View(), "poll: 1m0s") {
		t.Errorf("View missing poll cadence\n%s", m.View())
	}

	// Iteration 2 clears prior state (including Discovery seed).
	m.applyPhase(phaseEvent{kind: phaseRunning, iteration: time.Now()})
	if m.report.Passed != 0 || m.cats["ACL"] != nil || m.cats["Discovery"] != nil {
		t.Fatalf("iteration 2 did not reset: %+v / %+v", m.report, m.cats)
	}
	m.applyResult(Result{Status: "FAIL", Category: "MSSQL", Name: "sysadmin"})
	if m.report.Failed != 1 {
		t.Fatalf("iteration 2 fail count wrong: %+v", m.report)
	}
}
