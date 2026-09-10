package cmd

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestInspectorForSelectedLab(t *testing.T) {
	if _, ok := inspectorFor(&config.Config{Lab: "SCOPE-RANGE"}).(scopeRangeInspector); !ok {
		t.Fatal("SCOPE-RANGE must use its Linux workload inspector")
	}
	if _, ok := inspectorFor(&config.Config{Lab: "GOAD"}).(goadInspector); !ok {
		t.Fatal("GOAD must retain the Active Directory inspector")
	}
}
