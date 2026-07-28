package theme

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type apiFieldSpec struct {
	name      string
	swiftType string
	docType   string
}

var themeAPIV1DataFields = []apiFieldSpec{
	{"apiVersion", "Int", "readonly 1"},
	{"target", "Int64", "readonly number"},
	{"now", "Int64", "readonly number"},
	{"lead", "Int", "readonly number"},
	{"force", "Bool", "readonly boolean"},
	{"revealed", "Bool", "readonly boolean"},
	{"fired", "Bool", "readonly boolean"},
	{"screens", "Int", "readonly number"},
	{"screen", "ThemeAPIScreenV1", "readonly GongThemeScreenV1"},
}

var themeAPIV1ScreenFields = []apiFieldSpec{
	{"index", "Int", "readonly number"},
	{"isMain", "Bool", "readonly boolean"},
	{"primary", "Bool", "readonly boolean"},
	{"w", "Double", "readonly number"},
	{"h", "Double", "readonly number"},
	{"scale", "Double", "readonly number"},
}

var themeAPIV1Hooks = map[string]string{
	"onReveal": "(() => void) | null",
	"onTick":   "((now: number) => void) | null",
	"onFire":   "(() => void) | null",
	"done":     "method (): void",
}

func TestThemeAPIV1SchemaMatchesOverlayAndDocs(t *testing.T) {
	overlay := readRepoFile(t, "overlay.swift")
	doc := readRepoFile(t, "doc.md")

	if !strings.Contains(overlay, "let themeAPIVersion = 1") {
		t.Error("overlay.swift must declare Theme API major version 1")
	}
	if !strings.Contains(doc, "readonly apiVersion: 1;") {
		t.Error("doc.md must declare Theme API major version 1")
	}
	for _, token := range []string{
		"func epochMilliseconds(_ seconds: Double) -> Int64?",
		"epochMilliseconds(epoch) != nil",
		"Number.isSafeInteger(value)",
	} {
		if !strings.Contains(overlay, token) {
			t.Errorf("overlay.swift is missing Theme API time-safety guard %q", token)
		}
	}
	overlayRoot := swiftStructSchema(t, overlay, "ThemeAPIContextV1")
	overlayScreen := swiftStructSchema(t, overlay, "ThemeAPIScreenV1")
	assertSameSchema(t, "overlay window.gong", overlayRoot, expectedSwiftSchema(themeAPIV1DataFields))
	assertSameSchema(t, "overlay window.gong.screen", overlayScreen, expectedSwiftSchema(themeAPIV1ScreenFields))
	assertSameFields(t, "overlay window.gong callback slots",
		overlayCallbackSlots(overlay), []string{"onReveal", "onTick", "onFire"})
	if !strings.Contains(overlay, "gong.done = function () {") {
		t.Error("overlay.swift must install window.gong.done")
	}

	aliases := typescriptAliases(t, doc)
	docRoot := typescriptInterfaceSchema(t, doc, "GongThemeAPIV1", aliases)
	docScreen := typescriptInterfaceSchema(t, doc, "GongThemeScreenV1", aliases)
	wantDocRoot := expectedDocSchema(themeAPIV1DataFields)
	for name, signature := range themeAPIV1Hooks {
		wantDocRoot[name] = normalizeTypeScriptType(signature, aliases)
	}
	assertSameSchema(t, "doc.md window.gong", docRoot, wantDocRoot)
	assertSameSchema(t, "doc.md window.gong.screen", docScreen, expectedDocSchema(themeAPIV1ScreenFields))

	for _, class := range []string{"gong-live", "gong-fired"} {
		if !strings.Contains(doc, class) {
			t.Errorf("doc.md Theme API v1 is missing CSS class %q", class)
		}
	}
}

func TestThemeAPIV1LifecycleBridge(t *testing.T) {
	overlay := readRepoFile(t, "overlay.swift")
	if !strings.Contains(overlay, "var didReveal = false") || !strings.Contains(overlay, "var didFire = false") {
		t.Fatal("overlay.swift must keep private reveal/fire idempotency state")
	}

	reveal := sourceSection(t, overlay,
		"window.__gongReveal = function (ms) {",
		"window.__gongTick = function (ms) {")
	assertSourceOrder(t, "__gongReveal", reveal,
		"if (didReveal) return;",
		"didReveal = true;",
		"gong.now =",
		"gong.revealed = true;",
		"classList.add('gong-live')",
		"invoke('onReveal')")

	tick := sourceSection(t, overlay,
		"window.__gongTick = function (ms) {",
		"window.__gongFire = function (ms) {")
	assertSourceOrder(t, "__gongTick", tick,
		"if (!didReveal) return;",
		"gong.now =",
		"invoke('onTick', [gong.now])")

	fire := sourceSection(t, overlay,
		"window.__gongFire = function (ms) {",
		"window.addEventListener('error'")
	assertSourceOrder(t, "__gongFire", fire,
		"if (!didReveal) window.__gongReveal(ms);",
		"if (didFire) return;",
		"didFire = true;",
		"gong.now =",
		"gong.fired = true;",
		"classList.add('gong-fired')",
		"invoke('onFire')")

	done := sourceSection(t, overlay,
		"gong.done = function () {",
		"Object.defineProperty(gong, 'apiVersion'")
	assertSourceOrder(t, "window.gong.done", done,
		"if (doneSent || !gong.screen.primary) return;",
		"window.webkit.messageHandlers.done.postMessage(1);",
		"doneSent = true;")
}

func TestThemeAPIV1DoneMessageComesFromPrimaryMainFrame(t *testing.T) {
	overlay := readRepoFile(t, "overlay.swift")
	handler := sourceSection(t, overlay,
		"func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {",
		"// MARK: - 抑制条件")
	guard := strings.Join(strings.Fields(handler), " ")
	const originCheck = "guard !finished, m.frameInfo.isMainFrame, let source = m.webView, source === primaryWeb else { return }"
	if !strings.Contains(guard, originCheck) {
		t.Fatalf("done handler must reject non-main-frame and non-primary-WebView messages\nwant: %s", originCheck)
	}
	assertSourceOrder(t, "Swift done handler", handler,
		`guard m.name == "done" else {`,
		"m.frameInfo.isMainFrame",
		"source === primaryWeb",
		"finished = true",
		"NSApp.terminate(nil)")
}

func TestThemeAPIV1READMEPublicEntryPoints(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	for _, entry := range []string{
		"apiVersion: 1",
		"onReveal: null",
		"onTick: null",
		"onFire: null",
		"done() {}",
		"g.onReveal",
		"g.onTick",
		"g.onFire",
		"g.done()",
		"window.gong.screen.primary",
		"html.gong-live",
		"html.gong-fired",
	} {
		if !strings.Contains(readme, entry) {
			t.Errorf("README.md is missing Theme API v1 entry point %q", entry)
		}
	}
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate the repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var swiftStructFieldPattern = regexp.MustCompile(`(?m)^[ \t]*(let|var)[ \t]+([A-Za-z][A-Za-z0-9]*)[ \t]*:[ \t]*([^\r\n]+)`)

func swiftStructSchema(t *testing.T, source, name string) map[string]string {
	t.Helper()
	marker := "private struct " + name + ": Encodable {"
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("overlay.swift is missing %q", marker)
	}
	body := source[start+len(marker):]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("overlay.swift struct %s is not closed", name)
	}
	body = body[:end]
	fields := map[string]string{}
	for _, match := range swiftStructFieldPattern.FindAllStringSubmatch(body, -1) {
		fields[match[2]] = match[1] + " " + normalizeSwiftType(match[3])
	}
	if len(fields) == 0 {
		t.Fatalf("overlay.swift struct %s has no fields", name)
	}
	return fields
}

var overlayCallbackPattern = regexp.MustCompile(`(?m)^[ \t]*gong\.(on[A-Za-z0-9]+)[ \t]*=[ \t]*null;`)

func overlayCallbackSlots(source string) []string {
	var fields []string
	for _, match := range overlayCallbackPattern.FindAllStringSubmatch(source, -1) {
		fields = append(fields, match[1])
	}
	return fields
}

var (
	typescriptPropertyPattern = regexp.MustCompile(`^[ \t]*(readonly[ \t]+)?([A-Za-z][A-Za-z0-9]*)[ \t]*:[ \t]*(.+);[ \t]*$`)
	typescriptMethodPattern   = regexp.MustCompile(`^[ \t]*([A-Za-z][A-Za-z0-9]*)[ \t]*\(([^)]*)\)[ \t]*:[ \t]*(.+);[ \t]*$`)
	typescriptAliasPattern    = regexp.MustCompile(`(?m)^type[ \t]+([A-Za-z][A-Za-z0-9]*)[ \t]*=[ \t]*([^;]+);`)
)

func typescriptAliases(t *testing.T, doc string) map[string]string {
	t.Helper()
	aliases := map[string]string{}
	for _, match := range typescriptAliasPattern.FindAllStringSubmatch(doc, -1) {
		aliases[match[1]] = normalizeTypeScriptType(match[2], nil)
	}
	if got := aliases["EpochMilliseconds"]; got != "number" {
		t.Fatalf("doc.md must define EpochMilliseconds = number, got %q", got)
	}
	return aliases
}

func typescriptInterfaceSchema(t *testing.T, doc, name string, aliases map[string]string) map[string]string {
	t.Helper()
	marker := "interface " + name + " {"
	start := strings.Index(doc, marker)
	if start < 0 {
		t.Fatalf("doc.md Theme API v1 is missing %q", marker)
	}
	body := doc[start+len(marker):]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("doc.md interface %s is not closed", name)
	}
	body = body[:end]
	fields := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if match := typescriptPropertyPattern.FindStringSubmatch(line); match != nil {
			modifier := ""
			if match[1] != "" {
				modifier = "readonly "
			}
			fields[match[2]] = modifier + normalizeTypeScriptType(match[3], aliases)
			continue
		}
		if match := typescriptMethodPattern.FindStringSubmatch(line); match != nil {
			fields[match[1]] = "method (" + normalizeTypeScriptType(match[2], aliases) + "): " +
				normalizeTypeScriptType(match[3], aliases)
		}
	}
	if len(fields) == 0 {
		t.Fatalf("doc.md interface %s has no fields", name)
	}
	return fields
}

func expectedSwiftSchema(specs []apiFieldSpec) map[string]string {
	fields := map[string]string{}
	for _, spec := range specs {
		fields[spec.name] = "let " + spec.swiftType
	}
	return fields
}

func expectedDocSchema(specs []apiFieldSpec) map[string]string {
	fields := map[string]string{}
	for _, spec := range specs {
		fields[spec.name] = spec.docType
	}
	return fields
}

func normalizeSwiftType(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeTypeScriptType(value string, aliases map[string]string) string {
	value = strings.Join(strings.Fields(value), " ")
	for name, replacement := range aliases {
		value = strings.ReplaceAll(value, name, replacement)
	}
	return value
}

func sourceSection(t *testing.T, source, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("source is missing %q", startMarker)
	}
	rest := source[start:]
	end := strings.Index(rest, endMarker)
	if end < 0 {
		t.Fatalf("source section %q is missing end marker %q", startMarker, endMarker)
	}
	return rest[:end]
}

func assertSourceOrder(t *testing.T, label, source string, tokens ...string) {
	t.Helper()
	offset := 0
	for _, token := range tokens {
		at := strings.Index(source[offset:], token)
		if at < 0 {
			t.Fatalf("%s must contain %q after the preceding lifecycle step", label, token)
		}
		offset += at + len(token)
	}
}

func assertSameSchema(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	gotKeys := make([]string, 0, len(got))
	wantKeys := make([]string, 0, len(want))
	for key := range got {
		gotKeys = append(gotKeys, key)
	}
	for key := range want {
		wantKeys = append(wantKeys, key)
	}
	slices.Sort(gotKeys)
	slices.Sort(wantKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("%s fields drifted\n got: %v\nwant: %v", label, gotKeys, wantKeys)
	}
	for _, key := range wantKeys {
		if got[key] != want[key] {
			t.Fatalf("%s.%s type drifted\n got: %q\nwant: %q", label, key, got[key], want[key])
		}
	}
}

func assertSameFields(t *testing.T, label string, got, want []string) {
	t.Helper()
	got = slices.Clone(got)
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s fields drifted\n got: %v\nwant: %v", label, got, want)
	}
}
