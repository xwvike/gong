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
		Name:     "lunch",
		At:       "12:34:56",
		Weekdays: []int{1, 3, 5},
		Theme:    "default",
		Enabled:  true,
		Grace:    DefaultGrace,
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
		{name: "empty name", mutate: func(c *Config) { c.Schedules[0].Name = "" }, wantError: "没有名字"},
		{name: "unsafe name", mutate: func(c *Config) { c.Schedules[0].Name = "lunch/foo" }, wantError: "不能含空格"},
		{name: "duplicate name", mutate: func(c *Config) {
			c.Schedules = append(c.Schedules, c.Schedules[0])
		}, wantError: "定时名重复"},
		{name: "bad time", mutate: func(c *Config) { c.Schedules[0].At = "25:00" }, wantError: "时间越界"},
		{name: "no weekdays", mutate: func(c *Config) { c.Schedules[0].Weekdays = nil }, wantError: "一个星期几都没选"},
		{name: "weekday below range", mutate: func(c *Config) { c.Schedules[0].Weekdays = []int{-1} }, wantError: "星期几必须是 0 到 7"},
		{name: "weekday above range", mutate: func(c *Config) { c.Schedules[0].Weekdays = []int{8} }, wantError: "星期几必须是 0 到 7"},
		{name: "empty theme", mutate: func(c *Config) { c.Schedules[0].Theme = "" }, wantError: "没有主题"},
		{name: "unsafe theme", mutate: func(c *Config) { c.Schedules[0].Theme = "../default" }, wantError: "主题名不能含路径分隔符"},
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

			c := &Config{Version: 1, Schedules: []Schedule{validSchedule()}}
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

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := &Config{Version: 1, Schedules: []Schedule{validSchedule()}}
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

func TestLoadDefaultsZeroGraceAndRejectsInvalidValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	validTOML := `version = 1

[[schedule]]
name = "morning"
at = "08:00"
weekdays = [1, 2, 3, 4, 5]
theme = "default"
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
	if got := c.Schedules[0].Grace; got != DefaultGrace {
		t.Errorf("zero grace = %d, want default %d", got, DefaultGrace)
	}

	invalidTOML := strings.Replace(validTOML, "weekdays = [1, 2, 3, 4, 5]", "weekdays = [1, 8]", 1)
	if err := os.WriteFile(paths.ConfigFile(), []byte(invalidTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "星期几必须是 0 到 7") {
		t.Fatalf("Load() error = %v, want invalid weekday error", err)
	}
}

func TestSaveRejectsInvalidConfigBeforeWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (&Config{Version: 1, Schedules: []Schedule{{
		Name: "broken", At: "12:00", Weekdays: []int{1}, Theme: "default", Grace: -1,
	}}}).Save(); err == nil {
		t.Fatal("Save() should reject negative grace")
	}
	if Exists() {
		t.Fatal("Save() wrote a file after validation failed")
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
