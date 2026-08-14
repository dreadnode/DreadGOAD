package azure

import (
	"encoding/json"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
)

func strp(s string) *string { return &s }
func i32p(i int32) *int32   { return &i }

// The disk mapping is the half of DescribeInstance that needs no ARM
// transport: everything it reports comes off the VM's own StorageProfile, which
// is why describing a VM's disks costs no request beyond the VM Get.
func TestDisksFromProfileReadsOSAndDataDisks(t *testing.T) {
	premium := armcompute.StorageAccountTypesPremiumLRS
	caching := armcompute.CachingTypesReadWrite
	create := armcompute.DiskCreateOptionTypesFromImage

	sp := &armcompute.StorageProfile{
		OSDisk: &armcompute.OSDisk{
			Name:         strp("dc01-osdisk"),
			DiskSizeGB:   i32p(128),
			Caching:      &caching,
			CreateOption: &create,
			ManagedDisk: &armcompute.ManagedDiskParameters{
				ID:                 strp("/subscriptions/s/…/disks/dc01-osdisk"),
				StorageAccountType: &premium,
			},
		},
		DataDisks: []*armcompute.DataDisk{
			{Name: strp("dc01-data0"), Lun: i32p(0), DiskSizeGB: i32p(512)},
			nil, // the SDK yields pointers; a nil entry must not panic
		},
	}

	got := disksFromProfile(sp)
	if len(got) != 2 {
		t.Fatalf("disks = %d, want 2 (os + one data, nil skipped): %+v", len(got), got)
	}

	os := got[0]
	if os.Role != "os" || os.Name != "dc01-osdisk" {
		t.Errorf("os disk = %+v", os)
	}
	if os.SizeGB == nil || *os.SizeGB != 128 {
		t.Errorf("os size = %v, want 128", os.SizeGB)
	}
	if os.StorageType != "Premium_LRS" {
		t.Errorf("storage type = %q, want Premium_LRS", os.StorageType)
	}
	if os.Caching != "ReadWrite" || os.CreateOption != "FromImage" {
		t.Errorf("caching/create = %q/%q", os.Caching, os.CreateOption)
	}
	// The OS disk is not addressed by LUN; reporting 0 would imply it is.
	if os.Lun != nil {
		t.Errorf("os disk carries a LUN: %v", *os.Lun)
	}

	data := got[1]
	if data.Role != "data" || data.Lun == nil || *data.Lun != 0 {
		t.Errorf("data disk = %+v", data)
	}
}

func TestDisksFromProfileHandlesAbsentProfile(t *testing.T) {
	// A VM Get can come back without a storage profile. The panel renders the
	// list directly, so this must be an empty array rather than nil — nil
	// marshals to `null` and the UI would have to guard every field.
	for _, sp := range []*armcompute.StorageProfile{nil, {}} {
		got := disksFromProfile(sp)
		if got == nil {
			t.Fatal("disks = nil, want an empty slice")
		}
		if len(got) != 0 {
			t.Errorf("disks = %+v, want empty", got)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "[]" {
			t.Errorf("marshals to %s, want []", b)
		}
	}
}

// The console reads this payload, so the field names are an interface.
func TestInstanceDetailMarshalsStableFieldNames(t *testing.T) {
	b, err := json.Marshal(&InstanceDetail{
		ID: "/subscriptions/s/…/dc01", Name: "dc01", ResourceGroup: "rg",
		Disks: []DiskDetail{}, NICs: []NICDetail{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"id"`, `"name"`, `"resource_group"`, `"disks":[]`, `"nics":[]`,
	} {
		if !contains(string(b), want) {
			t.Errorf("payload %s is missing %s", b, want)
		}
	}
	// Optional fields stay out of the payload when unset rather than
	// rendering as empty strings the panel would have to filter.
	for _, absent := range []string{`"location"`, `"vm_size"`, `"power_state"`} {
		if contains(string(b), absent) {
			t.Errorf("payload %s should omit %s when unset", b, absent)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// PowerState was declared and rendered by the console panel while the Get that
// should populate it passed nil options, so the field was permanently empty.
// These pin the mapping now that the instance view is actually requested.
func TestPowerStateOfPicksTheStatusByPrefix(t *testing.T) {
	view := &armcompute.VirtualMachineInstanceView{
		Statuses: []*armcompute.InstanceViewStatus{
			// Provisioning state comes first in real responses; matching by
			// position instead of prefix would report "succeeded" as the power.
			{Code: strp("ProvisioningState/succeeded")},
			nil,
			{Code: nil},
			{Code: strp("PowerState/deallocated")},
		},
	}
	// "stopped", not "deallocated": the panel must agree with the graph node
	// beside it, which shows the same normalized vocabulary from ListInstances.
	if got := powerStateOf(view); got != "stopped" {
		t.Errorf("powerStateOf = %q, want stopped", got)
	}
}

func TestPowerStateOfIsEmptyWhenUnknown(t *testing.T) {
	// Absent instance view, and a view carrying no power status at all: both
	// must yield "" so omitempty drops the field rather than the panel
	// asserting a state Azure never reported.
	if got := powerStateOf(nil); got != "" {
		t.Errorf("powerStateOf(nil) = %q, want empty", got)
	}
	view := &armcompute.VirtualMachineInstanceView{
		Statuses: []*armcompute.InstanceViewStatus{{Code: strp("ProvisioningState/updating")}},
	}
	if got := powerStateOf(view); got != "" {
		t.Errorf("powerStateOf = %q, want empty", got)
	}
}
