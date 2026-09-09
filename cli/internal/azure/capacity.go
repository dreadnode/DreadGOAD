package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
)

// SKUStatus is what one requested VM size looks like in one region.
type SKUStatus struct {
	Name string
	// False when the region does not offer this size at all — a different
	// failure from being offered but restricted.
	Offered bool
	// Restrictions Azure reports against this subscription in this region.
	// "NotAvailableForSubscription" is the capacity-restriction case that
	// surfaces at apply time as SkuNotAvailable.
	Restrictions []string
	// vCPUs per instance, 0 when Azure does not report it. Used to turn a VM
	// count into the core count a quota is denominated in.
	VCPUs int32
	// Zones the SKU is restricted out of, when the restriction is zonal rather
	// than regional. A zonal restriction still leaves the region usable.
	RestrictedZones []string
}

// Blocked reports whether this SKU cannot currently be deployed region-wide.
//
// Zone-level restrictions are excluded on purpose: the lab's terragrunt units
// do not pin an availability zone, so Azure is free to place the VM in a zone
// that is not restricted.
func (s SKUStatus) Blocked() bool {
	return !s.Offered || len(s.Restrictions) > 0
}

// QuotaItem is one usage counter in a region.
type QuotaItem struct {
	Name    string
	Current int32
	Limit   int64
}

// Headroom is how much of this quota remains.
func (q QuotaItem) Headroom() int64 { return q.Limit - int64(q.Current) }

// SKUAvailability reports, for each requested VM size, whether the region
// currently offers it to this subscription.
//
// This is the read behind the SkuNotAvailable failure that only otherwise
// surfaces minutes into `tofu apply`: Azure publishes the same restriction on
// the Resource SKUs API before anything is created.
//
// One paged call covers every size, so the cost does not grow with the range.
// Sizes are matched case-insensitively — terragrunt files carry Azure's
// canonical casing ("Standard_D2s_v3") but nothing enforces it.
func (c *Client) SKUAvailability(ctx context.Context, region string, sizes []string) ([]SKUStatus, error) {
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}
	if region == "" {
		return nil, fmt.Errorf("region is required to check SKU availability")
	}

	want := make(map[string]*SKUStatus, len(sizes))
	order := make([]string, 0, len(sizes))
	for _, s := range sizes {
		key := strings.ToLower(s)
		if _, dup := want[key]; dup {
			continue
		}
		want[key] = &SKUStatus{Name: s}
		order = append(order, key)
	}
	if len(want) == 0 {
		return nil, nil
	}

	// The location filter is the only one this API supports, and it is what
	// keeps the response to the region's SKUs rather than every SKU on Azure.
	filter := fmt.Sprintf("location eq '%s'", region)
	pager := c.skuClient.NewListPager(&armcompute.ResourceSKUsClientListOptions{Filter: &filter})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list compute SKUs in %s: %w", region, err)
		}
		for _, sku := range page.Value {
			if sku == nil || sku.Name == nil {
				continue
			}
			// The filter narrows to the region, but a SKU can still be listed
			// with resourceType "disks" and the like; only VM sizes matter.
			if sku.ResourceType != nil && !strings.EqualFold(*sku.ResourceType, "virtualMachines") {
				continue
			}
			st, ok := want[strings.ToLower(*sku.Name)]
			if !ok {
				continue
			}
			st.Offered = true
			st.VCPUs = vcpusOf(sku.Capabilities)
			applyRestrictions(st, sku.Restrictions, region)
		}
	}

	out := make([]SKUStatus, 0, len(order))
	for _, key := range order {
		out = append(out, *want[key])
	}
	return out, nil
}

// applyRestrictions records the region-scoped restrictions on a SKU, keeping
// zonal ones separate so a zone-restricted size is not reported as unusable.
func applyRestrictions(st *SKUStatus, restrictions []*armcompute.ResourceSKURestrictions, region string) {
	for _, r := range restrictions {
		if r == nil || r.Type == nil {
			continue
		}
		reason := "restricted"
		if r.ReasonCode != nil {
			reason = string(*r.ReasonCode)
		}
		switch *r.Type {
		case armcompute.ResourceSKURestrictionsTypeLocation:
			// Values carries the restricted locations. Guard against a
			// restriction published for some other region in the same record.
			if !matchesRegion(r.Values, region) {
				continue
			}
			st.Restrictions = append(st.Restrictions, reason)
		case armcompute.ResourceSKURestrictionsTypeZone:
			if r.RestrictionInfo != nil {
				for _, z := range r.RestrictionInfo.Zones {
					if z != nil {
						st.RestrictedZones = append(st.RestrictedZones, *z)
					}
				}
			}
		}
	}
}

// matchesRegion reports whether a location restriction covers this region. An
// empty value list is treated as covering it: Azure returned the restriction
// under a location-filtered query, so the conservative reading is that it
// applies.
func matchesRegion(values []*string, region string) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		if v != nil && strings.EqualFold(*v, region) {
			return true
		}
	}
	return false
}

func vcpusOf(caps []*armcompute.ResourceSKUCapabilities) int32 {
	for _, cap := range caps {
		if cap == nil || cap.Name == nil || cap.Value == nil {
			continue
		}
		if strings.EqualFold(*cap.Name, "vCPUs") {
			if n, err := strconv.ParseInt(*cap.Value, 10, 32); err == nil {
				return int32(n)
			}
		}
	}
	return 0
}

// RegionQuota returns the compute usage counters for a region.
//
// Separate from SKU availability because they fail differently: a quota is a
// subscription limit you can raise by asking, while a capacity restriction is
// Azure having no hardware to give and is only fixed by waiting or moving.
func (c *Client) RegionQuota(ctx context.Context, region string) ([]QuotaItem, error) {
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}
	if region == "" {
		return nil, fmt.Errorf("region is required to check quota")
	}
	var out []QuotaItem
	pager := c.usageClient.NewListPager(region, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list compute usage in %s: %w", region, err)
		}
		for _, u := range page.Value {
			if u == nil || u.Name == nil || u.Limit == nil || u.CurrentValue == nil {
				continue
			}
			name := ""
			if u.Name.Value != nil {
				name = *u.Name.Value
			}
			out = append(out, QuotaItem{Name: name, Current: *u.CurrentValue, Limit: *u.Limit})
		}
	}
	return out, nil
}

// FindQuota returns the named usage counter, or false when Azure did not
// report it. Names are the API's invariant form ("cores", "virtualMachines").
func FindQuota(items []QuotaItem, name string) (QuotaItem, bool) {
	for _, q := range items {
		if strings.EqualFold(q.Name, name) {
			return q, true
		}
	}
	return QuotaItem{}, false
}
