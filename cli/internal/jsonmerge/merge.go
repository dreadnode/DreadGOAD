package jsonmerge

import (
	"encoding/json"
	"fmt"
)

// MergePatch applies an RFC 7386 JSON Merge Patch to a base document.
// Objects merge recursively; null in the patch deletes keys; arrays and
// scalars in the patch replace the base value wholesale.
func MergePatch(base, patch any) any {
	patchMap, patchIsObj := patch.(map[string]any)
	if !patchIsObj {
		return patch
	}

	baseMap, baseIsObj := base.(map[string]any)
	if !baseIsObj {
		baseMap = make(map[string]any)
	} else {
		// Shallow-copy so we don't mutate the caller's map.
		cp := make(map[string]any, len(baseMap))
		for k, v := range baseMap {
			cp[k] = v
		}
		baseMap = cp
	}

	for k, v := range patchMap {
		if v == nil {
			delete(baseMap, k)
		} else {
			baseMap[k] = MergePatch(baseMap[k], v)
		}
	}
	return baseMap
}

// MergePatchBytes merges base JSON with a patch document and returns
// the result as pretty-printed JSON bytes.
func MergePatchBytes(base, patch []byte) ([]byte, error) {
	var baseVal, patchVal any
	if err := json.Unmarshal(base, &baseVal); err != nil {
		return nil, fmt.Errorf("unmarshal base: %w", err)
	}
	if err := json.Unmarshal(patch, &patchVal); err != nil {
		return nil, fmt.Errorf("unmarshal patch: %w", err)
	}

	merged := MergePatch(baseVal, patchVal)
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged: %w", err)
	}
	return append(out, '\n'), nil
}
