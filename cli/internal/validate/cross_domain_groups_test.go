package validate

import (
	"reflect"
	"testing"
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
