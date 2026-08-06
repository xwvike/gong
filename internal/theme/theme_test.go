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

func TestThemeAttributionMetadata(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bloom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := `author = "alice"
source = "https://github.com/alice/original_project"
`
	if err := os.WriteFile(filepath.Join(dir, "theme.toml"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}

	th, err := load(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Attribution(); got != "bloom  @alice  https://github.com/alice/original_project" {
		t.Fatalf("主题归属信息 = %q", got)
	}
}

func TestThemeAttributionIgnoresInvalidSource(t *testing.T) {
	th := Theme{ID: "local", Meta: Meta{Author: "@alice", Source: "not a URL"}}
	if th.AuthorLabel() != "@alice" {
		t.Fatalf("已有 @ 的作者名被重复处理：%q", th.AuthorLabel())
	}
	if th.SourceURL() != "" || th.Attribution() != "local  @alice" {
		t.Fatalf("非法来源不应进入展示或浏览器：%q", th.Attribution())
	}
}

func TestBuiltinThemeAttributions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBuiltin := paths.BuiltinThemes
	paths.BuiltinThemes = filepath.Join("..", "..", "themes")
	t.Cleanup(func() { paths.BuiltinThemes = oldBuiltin })

	want := map[string]string{
		"bloom":  "bloom  @Arlan  https://www.arlan.me/vault",
		"chroma": "chroma  @Arlan  https://www.arlan.me/vault/chroma-glow",
		"led":    "led  @originkit  https://www.originkit.dev/components/pixel-led-display?from=%2Fcategory%2Ftext&preset=base",
		"noise":  "noise  @originkit  https://www.originkit.dev/components/text-noise?from=%2Fcategory%2Ftext&preset=base",
		"tunnel": "tunnel  @originkit  https://www.originkit.dev/components/infinite-text-passage?from=%2Fcategory%2Ftext&preset=base",
	}
	got := make(map[string]string)
	for _, th := range List() {
		if th.ID == "dotcut" {
			t.Fatal("dotcut 已移除，不应再出现在内置主题列表")
		}
		got[th.ID] = th.Attribution()
	}
	for id, attribution := range want {
		if got[id] != attribution {
			t.Errorf("内置主题 %s 的归属信息 = %q，想要 %q", id, got[id], attribution)
		}
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
