package theme

import "testing"

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
