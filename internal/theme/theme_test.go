package theme

import (
	"math"
	"testing"
)

func TestResolveRejectsPathTraversal(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../default", "nested/theme"} {
		t.Run(id, func(t *testing.T) {
			if _, err := Resolve(id); err == nil {
				t.Fatalf("Resolve(%q) should reject an invalid theme id", id)
			}
		})
	}
}

func TestThemeLimits(t *testing.T) {
	th := Theme{Meta: Meta{Lead: 999, Duration: 999}}
	if got := th.LeadSeconds(); got != MaxLead {
		t.Errorf("LeadSeconds() = %d, want %d", got, MaxLead)
	}
	if got := th.TimeoutSeconds(); got != MaxVisible {
		t.Errorf("TimeoutSeconds() = %d, want %d", got, MaxVisible)
	}
}

func TestLegacyThemeMetadataDefaults(t *testing.T) {
	th := Theme{ID: "legacy"}
	if got := th.LeadSeconds(); got != 0 {
		t.Errorf("LeadSeconds() = %d, want 0", got)
	}
	if got := th.TimeoutSeconds(); got != 20 {
		t.Errorf("TimeoutSeconds() = %d, want 20", got)
	}
	if th.Meta.Desc != "" || th.Meta.Placement != "" || th.Meta.WebGL {
		t.Fatalf("legacy metadata defaults changed: %+v", th.Meta)
	}
}

func TestThemeTimeoutSaturatesBeforeIntegerOverflow(t *testing.T) {
	th := Theme{Meta: Meta{Duration: math.MaxInt}}
	if got := th.TimeoutSeconds(); got != MaxVisible {
		t.Errorf("TimeoutSeconds() = %d, want %d", got, MaxVisible)
	}
}
