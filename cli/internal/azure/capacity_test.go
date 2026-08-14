package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
)

func restrictionsFor(t armcompute.ResourceSKURestrictionsType, reason armcompute.ResourceSKURestrictionsReasonCode,
	values []string, zones []string,
) []*armcompute.ResourceSKURestrictions {
	vals := make([]*string, len(values))
	for i := range values {
		vals[i] = &values[i]
	}
	zs := make([]*string, len(zones))
	for i := range zones {
		zs[i] = &zones[i]
	}
	return []*armcompute.ResourceSKURestrictions{{
		Type:            &t,
		ReasonCode:      &reason,
		Values:          vals,
		RestrictionInfo: &armcompute.ResourceSKURestrictionInfo{Zones: zs},
	}}
}

// The failure this whole check exists for: eastus published
// NotAvailableForSubscription for Standard_D2s_v3, and `up` only discovered it
// minutes into apply.
func TestLocationRestrictionMarksTheSKUBlocked(t *testing.T) {
	st := &SKUStatus{Name: "Standard_D2s_v3", Offered: true}
	applyRestrictions(st, restrictionsFor(
		armcompute.ResourceSKURestrictionsTypeLocation,
		armcompute.ResourceSKURestrictionsReasonCodeNotAvailableForSubscription,
		[]string{"eastus"}, nil,
	), "eastus")

	if !st.Blocked() {
		t.Fatal("a location restriction for this region must block the SKU")
	}
	if len(st.Restrictions) != 1 || st.Restrictions[0] != "NotAvailableForSubscription" {
		t.Errorf("restrictions = %v, want the reason code", st.Restrictions)
	}
}

// A record can carry a restriction scoped to some other region. Treating it as
// ours would warn about a region that is fine.
func TestLocationRestrictionForAnotherRegionIsIgnored(t *testing.T) {
	st := &SKUStatus{Name: "Standard_D2s_v3", Offered: true}
	applyRestrictions(st, restrictionsFor(
		armcompute.ResourceSKURestrictionsTypeLocation,
		armcompute.ResourceSKURestrictionsReasonCodeNotAvailableForSubscription,
		[]string{"westeurope"}, nil,
	), "eastus")

	if st.Blocked() {
		t.Errorf("restriction on westeurope must not block eastus: %v", st.Restrictions)
	}
}

// Zonal restrictions are not blockers: the lab's units pin no zone, so Azure
// places the VM in one that is not restricted. Reporting these as blocked would
// warn on almost every deploy and train the operator to ignore the check.
func TestZoneRestrictionIsRecordedButNotBlocking(t *testing.T) {
	st := &SKUStatus{Name: "Standard_D2s_v3", Offered: true}
	applyRestrictions(st, restrictionsFor(
		armcompute.ResourceSKURestrictionsTypeZone,
		armcompute.ResourceSKURestrictionsReasonCodeNotAvailableForSubscription,
		[]string{"eastus"}, []string{"1", "3"},
	), "eastus")

	if st.Blocked() {
		t.Error("a zone restriction must not block a region-wide deploy")
	}
	if len(st.RestrictedZones) != 2 {
		t.Errorf("restricted zones = %v, want 1 and 3", st.RestrictedZones)
	}
}

// A SKU the region does not list at all never gets Offered set. That is a
// different failure from "offered but restricted" and must still block.
func TestUnofferedSKUIsBlocked(t *testing.T) {
	st := SKUStatus{Name: "Standard_D2s_v3"}
	if !st.Blocked() {
		t.Error("a SKU the region does not offer must be blocked")
	}
}

// An empty Values list under a location-filtered query is read as applying
// here — the conservative direction for a warning.
func TestLocationRestrictionWithNoValuesApplies(t *testing.T) {
	st := &SKUStatus{Name: "Standard_D2s_v3", Offered: true}
	applyRestrictions(st, restrictionsFor(
		armcompute.ResourceSKURestrictionsTypeLocation,
		armcompute.ResourceSKURestrictionsReasonCodeQuotaID,
		nil, nil,
	), "eastus")
	if !st.Blocked() {
		t.Error("a location restriction with no values must be treated as applying")
	}
}

func TestVCPUsParsedFromCapabilities(t *testing.T) {
	name, val := "vCPUs", "2"
	other, otherVal := "MemoryGB", "8"
	caps := []*armcompute.ResourceSKUCapabilities{
		{Name: &other, Value: &otherVal},
		{Name: &name, Value: &val},
		nil, // the SDK yields pointers; a nil entry must not panic
	}
	if got := vcpusOf(caps); got != 2 {
		t.Errorf("vcpusOf = %d, want 2", got)
	}
	// Absent or unparseable capabilities yield 0, which the caller treats as
	// "cannot estimate" rather than "needs no cores".
	if got := vcpusOf(nil); got != 0 {
		t.Errorf("vcpusOf(nil) = %d, want 0", got)
	}
	bad := "not-a-number"
	if got := vcpusOf([]*armcompute.ResourceSKUCapabilities{{Name: &name, Value: &bad}}); got != 0 {
		t.Errorf("vcpusOf(unparseable) = %d, want 0", got)
	}
}

func TestQuotaHeadroomAndLookup(t *testing.T) {
	items := []QuotaItem{
		{Name: "cores", Current: 10, Limit: 20},
		{Name: "virtualMachines", Current: 3, Limit: 100},
	}
	cores, ok := FindQuota(items, "Cores") // case-insensitive: the API varies
	if !ok {
		t.Fatal("cores not found")
	}
	if cores.Headroom() != 10 {
		t.Errorf("headroom = %d, want 10", cores.Headroom())
	}
	if _, ok := FindQuota(items, "nope"); ok {
		t.Error("FindQuota reported a counter that is not there")
	}
}
