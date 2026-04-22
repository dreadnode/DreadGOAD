package jsonmerge

import (
	"encoding/json"
	"fmt"
)

// Diff computes an RFC 7386 JSON Merge Patch that, when applied to base
// via MergePatch, produces target.  Only differing paths appear in the
// result; removed keys are represented as null.
func Diff(base, target any) any {
	baseMap, baseIsObj := base.(map[string]any)
	targetMap, targetIsObj := target.(map[string]any)

	if !baseIsObj || !targetIsObj {
		return target
	}

	patch := make(map[string]any)

	// Keys in target that differ from base.
	for k, tv := range targetMap {
		bv, exists := baseMap[k]
		if !exists {
			patch[k] = tv
			continue
		}
		_, bIsObj := bv.(map[string]any)
		_, tIsObj := tv.(map[string]any)
		if bIsObj && tIsObj {
			sub := Diff(bv, tv)
			if subMap, ok := sub.(map[string]any); ok && len(subMap) > 0 {
				patch[k] = sub
			}
		} else if !jsonEqual(bv, tv) {
			patch[k] = tv
		}
	}

	// Keys removed in target.
	for k := range baseMap {
		if _, exists := targetMap[k]; !exists {
			patch[k] = nil
		}
	}

	return patch
}

// DiffBytes computes an overlay patch from base to target JSON bytes.
func DiffBytes(base, target []byte) ([]byte, error) {
	var baseVal, targetVal any
	if err := json.Unmarshal(base, &baseVal); err != nil {
		return nil, fmt.Errorf("unmarshal base: %w", err)
	}
	if err := json.Unmarshal(target, &targetVal); err != nil {
		return nil, fmt.Errorf("unmarshal target: %w", err)
	}

	patch := Diff(baseVal, targetVal)

	out, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal patch: %w", err)
	}
	return append(out, '\n'), nil
}

func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
