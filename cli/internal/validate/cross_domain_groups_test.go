package validate

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/labmap"
)

func TestParseCrossDomainMembershipOutput(t *testing.T) {
	output := `unrelated diagnostic output
MEMBER_OK=essos.local\daenerys.targaryen
MEMBER_MISSING=sevenkingdoms.local\tyron.lannister
MEMBER_LOOKUP_ERROR=sevenkingdoms.local\DragonRider
MEMBER_INVALID=malformed-member
MEMBER_OK=essos.local\greyworm`

	got := parseCrossDomainMembershipOutput(output)
	want := crossDomainMembershipProbeResult{
		okCount: 2,
		missing: []string{
			`sevenkingdoms.local\tyron.lannister`,
		},
		unresolved: []string{
			`sevenkingdoms.local\DragonRider`,
			"malformed-member",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCrossDomainMembershipOutput() = %#v, want %#v", got, want)
	}
}

func TestReportCrossDomainMembershipResult_MissingTakesPrecedence(t *testing.T) {
	v := &Validator{report: Report{}, silent: true}
	gf := labmap.CrossDomainGroupFact{
		Group:   "DragonsFriends",
		Domain:  "essos.local",
		Members: []string{"missing-user", "unresolved-user"},
	}
	result := crossDomainMembershipProbeResult{
		missing:    []string{"missing-user"},
		unresolved: []string{"unresolved-user"},
	}

	var output bytes.Buffer
	v.reportCrossDomainMembershipResult(&output, gf, result)

	if v.report.Failed != 1 || v.report.Warnings != 0 {
		t.Fatalf("report counts = failed:%d warnings:%d, want failed:1 warnings:0", v.report.Failed, v.report.Warnings)
	}
	if len(v.report.Results) != 1 || v.report.Results[0].Status != "FAIL" {
		t.Fatalf("report results = %#v, want one FAIL", v.report.Results)
	}
	message := v.report.Results[0].Name
	for _, detail := range []string{"missing-user", "unresolved-user"} {
		if !strings.Contains(message, detail) {
			t.Errorf("result message %q does not contain %q", message, detail)
		}
	}
}
