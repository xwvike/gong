package theme

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/xwvike/gong/internal/paths"
)

func TestResolveRejectsPathTraversal(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../led", "nested/theme"} {
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
}

func TestThemeTimeoutSaturatesBeforeIntegerOverflow(t *testing.T) {
	th := Theme{Meta: Meta{Duration: math.MaxInt}}
	if got := th.TimeoutSeconds(); got != MaxVisible {
		t.Errorf("TimeoutSeconds() = %d, want %d", got, MaxVisible)
	}
}

func TestBrokenUserOverrideHidesBuiltinTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	builtin := t.TempDir()
	oldBuiltin := paths.BuiltinThemes
	paths.BuiltinThemes = builtin
	t.Cleanup(func() { paths.BuiltinThemes = oldBuiltin })

	builtinTheme := filepath.Join(builtin, "led")
	userTheme := filepath.Join(paths.UserThemes(), "led")
	for _, dir := range []string{builtinTheme, userTheme} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(builtinTheme, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userTheme, "theme.toml"), []byte("lead = nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve("led"); err == nil {
		t.Fatal("损坏的用户覆盖应该使 Resolve 失败")
	}
	for _, th := range List() {
		if th.ID == "led" {
			t.Fatal("List 不应显示被损坏用户目录遮住的内置主题")
		}
	}
}

func TestDefaultIsLegacyAliasForLED(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	builtin := t.TempDir()
	oldBuiltin := paths.BuiltinThemes
	paths.BuiltinThemes = builtin
	t.Cleanup(func() { paths.BuiltinThemes = oldBuiltin })

	for _, id := range []string{"led", "default", "@random"} {
		dir := filepath.Join(builtin, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(id), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	th, err := Resolve("default")
	if err != nil {
		t.Fatal(err)
	}
	if th.ID != "led" {
		t.Fatalf("default 应解析到 led，拿到 %q", th.ID)
	}
	for _, listed := range List() {
		if listed.ID == "default" || listed.ID == "@random" {
			t.Fatalf("主题列表不应暴露保留名称 %q", listed.ID)
		}
	}
	if _, err := Resolve("@random"); err == nil {
		t.Fatal("主题策略不应被当作真实主题解析")
	}
}
