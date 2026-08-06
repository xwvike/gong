package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xwvike/gong/internal/paths"
)

func validSchedule() Schedule {
	return Schedule{
		Label:    "lunch",
		At:       "12:34:56",
		Weekdays: []int{1, 3, 5},
		Theme:    DefaultTheme,
		Enabled:  true,
		Grace:    DefaultGrace,
	}
}

func TestDefaultSchedulesUseRandomThemes(t *testing.T) {
	c := Default()
	if len(c.Schedules) != 2 {
		t.Fatalf("默认定时数量 = %d，想要 2", len(c.Schedules))
	}
	for i, schedule := range c.Schedules {
		if schedule.Theme != ThemeRandom {
			t.Errorf("默认定时 #%d 的主题 = %q，想要 %q", i+1, schedule.Theme, ThemeRandom)
		}
	}
}

func TestParseClock(t *testing.T) {
	tests := []struct {
		input string
		want  int
		ok    bool
	}{
		{input: "00:00", want: 0, ok: true},
		{input: "23:59:59", want: 86399, ok: true},
		{input: " 08:07:06 ", want: 8*3600 + 7*60 + 6, ok: true},
		{input: "24:00", ok: false},
		{input: "12:60", ok: false},
		{input: "12:00:60", ok: false},
		{input: "12", ok: false},
		{input: "12:00:00:01", ok: false},
		{input: "12:x0", ok: false},
		{input: "-1:00", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseClock(tc.input)
			if tc.ok {
				if err != nil {
					t.Fatalf("ParseClock(%q) error = %v", tc.input, err)
				}
				if got != tc.want {
					t.Errorf("ParseClock(%q) = %d, want %d", tc.input, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Errorf("ParseClock(%q) should reject invalid input", tc.input)
			}
		})
	}
}

func TestFormatClockNormalizesSeconds(t *testing.T) {
	tests := map[int]string{
		0:         "00:00:00",
		86399:     "23:59:59",
		86400:     "00:00:00",
		-1:        "23:59:59",
		-86401:    "23:59:59",
		25 * 3600: "01:00:00",
	}
	for input, want := range tests {
		if got := FormatClock(input); got != want {
			t.Errorf("FormatClock(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantError string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "nil config", mutate: nil, wantError: "配置不能为空"},
		{name: "unsupported version", mutate: func(c *Config) { c.Version = 2 }, wantError: "不支持配置版本"},
		{name: "empty label is fine", mutate: func(c *Config) { c.Schedules[0].Label = "" }},
		{name: "label with spaces is fine", mutate: func(c *Config) { c.Schedules[0].Label = "my lunch" }},
		{name: "duplicate label is fine", mutate: func(c *Config) {
			c.Schedules = append(c.Schedules, c.Schedules[0])
		}},
		{name: "bad time", mutate: func(c *Config) { c.Schedules[0].At = "25:00" }, wantError: "时间越界"},
		{name: "no weekdays", mutate: func(c *Config) { c.Schedules[0].Weekdays = nil }, wantError: "一个星期几都没选"},
		{name: "weekday below range", mutate: func(c *Config) { c.Schedules[0].Weekdays = []int{-1} }, wantError: "星期几必须是 0 到 7"},
		{name: "weekday above range", mutate: func(c *Config) { c.Schedules[0].Weekdays = []int{8} }, wantError: "星期几必须是 0 到 7"},
		{name: "empty theme", mutate: func(c *Config) { c.Schedules[0].Theme = "" }, wantError: "没有主题"},
		{name: "unsafe theme", mutate: func(c *Config) { c.Schedules[0].Theme = "../led" }, wantError: "主题名不能含路径分隔符"},
		{name: "negative grace", mutate: func(c *Config) { c.Schedules[0].Grace = -1 }, wantError: "grace 不能是负数"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mutate == nil {
				var c *Config
				if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Validate() error = %v, want %q", err, tc.wantError)
				}
				return
			}

			c := &Config{Version: CurrentVersion, Schedules: []Schedule{validSchedule()}}
			tc.mutate(c)
			err := c.Validate()
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}

// 错误信息有标签时用标签，否则用序号。
func TestValidateErrorUsesDisplayName(t *testing.T) {
	c := &Config{Version: CurrentVersion, Schedules: []Schedule{
		{Label: "午间", At: "12:00", Weekdays: []int{1}, Theme: DefaultTheme, Grace: -1},
	}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "午间") {
		t.Fatalf("有标签时应该报标签，got %v", err)
	}

	c.Schedules[0].Label = ""
	err = c.Validate()
	if err == nil || !strings.Contains(err.Error(), "#1") {
		t.Fatalf("没标签时应该落回 #1，got %v", err)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := &Config{Version: CurrentVersion, Schedules: []Schedule{validSchedule()}}
	if err := want.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != want.Version || len(got.Schedules) != 1 {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(got.Schedules[0], want.Schedules[0]) {
		t.Errorf("loaded schedule = %#v, want %#v", got.Schedules[0], want.Schedules[0])
	}
}

func TestLoadMigratesDefaultThemeToLED(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `version = 1

[[schedule]]
at = "12:00"
weekdays = [1]
theme = "default"
enabled = true
grace = 1200
`
	if err := os.WriteFile(paths.ConfigFile(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Schedules[0].Theme; got != DefaultTheme {
		t.Errorf("旧主题迁移后 = %q，想要 %q", got, DefaultTheme)
	}
}

func TestThemeStrategyLabels(t *testing.T) {
	if !IsThemeStrategy(ThemeRandom) || !IsThemeStrategy(ThemeSequence) || IsThemeStrategy(DefaultTheme) {
		t.Fatal("主题策略识别不正确")
	}
	if ThemeLabel(ThemeRandom) != "随机" || ThemeLabel(ThemeSequence) != "顺序" {
		t.Fatal("主题策略没有使用面向用户的名称")
	}
	if ThemeLabel("default") != DefaultTheme {
		t.Fatal("旧主题名没有按新名称展示")
	}
}

func TestThemeStrategySaveAndLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &Config{Version: CurrentVersion, Schedules: []Schedule{validSchedule()}}
	c.Schedules[0].Theme = ThemeSequence
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Schedules[0].Theme; got != ThemeSequence {
		t.Fatalf("顺序策略往返后 = %q", got)
	}
}

func TestConcurrentSavesUseIndependentTemporaryFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &Config{Version: CurrentVersion, Schedules: []Schedule{validSchedule()}}
	const writers = 8
	errs := make(chan error, writers)
	for range writers {
		go func() { errs <- c.Save() }()
	}
	for range writers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Save() error = %v", err)
		}
	}
	if _, err := Load(); err != nil {
		t.Fatalf("concurrent saves left invalid config: %v", err)
	}
	entries, err := os.ReadDir(paths.ConfigDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config.toml.tmp-") {
			t.Errorf("temporary config left behind: %s", entry.Name())
		}
	}
}

// 旧版 name 字段应被忽略并回落为无标签定时。
func TestLoadIgnoresLegacyNameField(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyTOML := `version = 1

[[schedule]]
name = "noon"
at = "12:00:00"
weekdays = [1, 2, 3, 4, 5]
theme = "led"
enabled = true
grace = 1200
`
	if err := os.WriteFile(paths.ConfigFile(), []byte(legacyTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() 应该能读旧版配置，error = %v", err)
	}
	if got := c.Schedules[0].Label; got != "" {
		t.Errorf("旧版 name 字段不该被误读成 Label，got %q", got)
	}
	if got := c.Schedules[0].Ref(0); got != "#1" {
		t.Errorf("没标签时 Ref = %q，想要 #1", got)
	}
}

func TestLoadPreservesExplicitZeroGrace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	validTOML := `version = 1

[[schedule]]
label = "morning"
at = "08:00"
weekdays = [1, 2, 3, 4, 5]
theme = "led"
enabled = true
grace = 0
`
	if err := os.WriteFile(paths.ConfigFile(), []byte(validTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error for valid config = %v", err)
	}
	if got := c.Schedules[0].Grace; got != 0 {
		t.Errorf("zero grace = %d, want 0", got)
	}
}

func TestLoadDefaultsMissingGrace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	tomlText := `version = 1

[[schedule]]
label = "morning"
at = "08:00"
weekdays = [1, 2, 3, 4, 5]
theme = "led"
enabled = true
`
	if err := os.WriteFile(paths.ConfigFile(), []byte(tomlText), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Schedules[0].Grace; got != DefaultGrace {
		t.Errorf("missing grace = %d, want %d", got, DefaultGrace)
	}
}

func TestLoadRejectsInvalidWeekday(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	invalidTOML := `version = 1

[[schedule]]
label = "morning"
at = "08:00"
weekdays = [1, 8]
theme = "led"
enabled = true
grace = 0
`

	if err := os.WriteFile(paths.ConfigFile(), []byte(invalidTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "星期几必须是 0 到 7") {
		t.Fatalf("Load() error = %v, want invalid weekday error", err)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	text := `version = 2

[[schedule]]
at = "08:00"
weekdays = [1]
theme = "led"
enabled = true
grace = 0
`
	if err := os.WriteFile(paths.ConfigFile(), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "不支持配置版本") {
		t.Fatalf("Load() error = %v, want unsupported version error", err)
	}
}

func TestSaveRejectsInvalidConfigBeforeWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (&Config{Version: CurrentVersion, Schedules: []Schedule{{
		At: "12:00", Weekdays: []int{1}, Theme: DefaultTheme, Grace: -1,
	}}}).Save(); err == nil {
		t.Fatal("Save() should reject negative grace")
	}
	if Exists() {
		t.Fatal("Save() wrote a file after validation failed")
	}
}

func TestAtAndRemoveAtAreIndexBased(t *testing.T) {
	c := &Config{Schedules: []Schedule{
		{Label: "a", At: "12:00", Weekdays: []int{1}, Theme: DefaultTheme, Grace: DefaultGrace},
		{Label: "b", At: "18:00", Weekdays: []int{1}, Theme: DefaultTheme, Grace: DefaultGrace},
	}}

	if _, ok := c.At(-1); ok {
		t.Error("At(-1) 应该越界失败")
	}
	if _, ok := c.At(2); ok {
		t.Error("At(2) 应该越界失败（只有 2 条，下标 0..1）")
	}
	s, ok := c.At(1)
	if !ok || s.Label != "b" {
		t.Fatalf("At(1) = %v, %v，想要 b", s, ok)
	}

	if c.RemoveAt(5) {
		t.Error("越界的 RemoveAt 应该返回 false")
	}
	if !c.RemoveAt(0) {
		t.Fatal("RemoveAt(0) 应该成功")
	}
	if len(c.Schedules) != 1 || c.Schedules[0].Label != "b" {
		t.Fatalf("删除后应该只剩 b，got %v", c.Schedules)
	}
}

func TestDisplayNameFallsBackToIndex(t *testing.T) {
	s := Schedule{}
	if got := s.Ref(0); got != "#1" {
		t.Errorf("Ref(0) 空标签 = %q，想要 #1", got)
	}
	s.Label = "午间"
	if got := s.Ref(0); got != "午间" {
		t.Errorf("Ref(0) 有标签 = %q，想要 午间", got)
	}
}

func TestEnsureUserThemeDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := EnsureUserThemeDir(); err != nil {
		t.Fatalf("EnsureUserThemeDir() error = %v", err)
	}
	info, err := os.Stat(paths.UserThemes())
	if err != nil {
		t.Fatalf("theme directory missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("theme path %q is not a directory", paths.UserThemes())
	}
}
