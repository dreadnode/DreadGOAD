package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

// Account/Group are what let the web app show which cloud account and resource
// group a range lives in. They come from data discovery already fetches (the
// EC2 Reservation's OwnerId, the Azure resource ID), so the contract that
// matters is: carried through verbatim when known, and *absent* — not empty
// string — when the provider can't determine them.
func TestStatusJSONAccountAndGroup(t *testing.T) {
	t.Run("azure carries both", func(t *testing.T) {
		b, err := instancesToStatusJSON([]provider.Instance{{
			ID:      "/subscriptions/70a9c8a4/resourceGroups/RG1/providers/x",
			Name:    "vm1",
			State:   "running",
			Account: "70a9c8a4",
			Group:   "RG1",
		}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got []statusJSONInstance
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got[0].Account != "70a9c8a4" {
			t.Errorf("account = %q, want 70a9c8a4", got[0].Account)
		}
		if got[0].Group != "RG1" {
			t.Errorf("group = %q, want RG1", got[0].Group)
		}
	})

	t.Run("aws carries account but no group", func(t *testing.T) {
		b, err := instancesToStatusJSON([]provider.Instance{{
			ID: "i-0abc", Name: "vm1", State: "running", Account: "123456789012",
		}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		raw := string(b)
		if !strings.Contains(raw, `"account": "123456789012"`) {
			t.Errorf("account missing from JSON: %s", raw)
		}
		// omitempty: AWS has no resource-group concept, so the key must be
		// absent rather than present-and-empty. A consumer can then tell
		// "not applicable" from "known to be blank".
		if strings.Contains(raw, `"group"`) {
			t.Errorf("group should be omitted when empty: %s", raw)
		}
	})

	t.Run("unknown account omits the key entirely", func(t *testing.T) {
		b, err := instancesToStatusJSON([]provider.Instance{{
			ID: "vmid-1", Name: "vm1", State: "running",
		}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		raw := string(b)
		if strings.Contains(raw, `"account"`) || strings.Contains(raw, `"group"`) {
			t.Errorf("unknown fields must be omitted, got: %s", raw)
		}
		// The pre-existing fields must still be present and unchanged, so an
		// older consumer is unaffected by the addition.
		for _, want := range []string{`"name"`, `"id"`, `"state"`, `"private_ip"`} {
			if !strings.Contains(raw, want) {
				t.Errorf("existing field %s dropped: %s", want, raw)
			}
		}
	})
}
