package theme

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestThemeAPIV1Contract(t *testing.T) {
	overlay := readRepoFile(t, "overlay.swift")
	for _, token := range []string{
		"let themeAPIVersion = 1",
		"let target: Int64",
		"let now: Int64",
		"let lead: Int",
		"let force: Bool",
		"let revealed: Bool",
		"let fired: Bool",
		"let screens: Int",
		"let screen: ThemeAPIScreenV1",
		"let index: Int",
		"let isMain: Bool",
		"let primary: Bool",
		"let w: Double",
		"let h: Double",
		"let scale: Double",
		"func epochMilliseconds(_ seconds: Double) -> Int64?",
		"Number.isSafeInteger(value)",
		"gong.onReveal = null",
		"gong.onTick = null",
		"gong.onFire = null",
		"gong.done = function ()",
		"Object.defineProperty(window, 'gong'",
	} {
		if !strings.Contains(overlay, token) {
			t.Errorf("overlay.swift 缺少 Theme API v1 契约 %q", token)
		}
	}
}

func TestThemeAPIV1LifecycleOrder(t *testing.T) {
	overlay := readRepoFile(t, "overlay.swift")
	reveal := sourceSection(t, overlay,
		"window.__gongReveal = function (ms) {",
		"window.__gongTick = function (ms) {")
	assertSourceOrder(t, reveal,
		"if (didReveal) return;",
		"didReveal = true;",
		"gong.now = normalizeMillis(ms);",
		"gong.revealed = true;",
		"classList.add('gong-live')",
		"invoke('onReveal')")

	fire := sourceSection(t, overlay,
		"window.__gongFire = function (ms) {",
		"window.addEventListener('error'")
	assertSourceOrder(t, fire,
		"if (!didReveal) window.__gongReveal(ms);",
		"if (didFire) return;",
		"didFire = true;",
		"gong.now = normalizeMillis(ms);",
		"gong.fired = true;",
		"classList.add('gong-fired')",
		"invoke('onFire')")

	handler := sourceSection(t, overlay,
		"func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {",
		"// MARK: - 抑制条件")
	assertSourceOrder(t, handler,
		`guard m.name == "done" else {`,
		"m.frameInfo.isMainFrame",
		"source === primaryWeb",
		"NSApp.terminate(nil)")
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位仓库")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sourceSection(t *testing.T, source, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("缺少 %q", startMarker)
	}
	rest := source[start:]
	end := strings.Index(rest, endMarker)
	if end < 0 {
		t.Fatalf("%q 后缺少 %q", startMarker, endMarker)
	}
	return rest[:end]
}

func assertSourceOrder(t *testing.T, source string, tokens ...string) {
	t.Helper()
	offset := 0
	for _, token := range tokens {
		at := strings.Index(source[offset:], token)
		if at < 0 {
			t.Fatalf("生命周期顺序中缺少 %q", token)
		}
		offset += at + len(token)
	}
}
