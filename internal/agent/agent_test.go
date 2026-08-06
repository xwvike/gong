package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
)

func TestComputeTrigger(t *testing.T) {
	workdays := []int{1, 2, 3, 4, 5}
	cases := []struct {
		name     string
		at       int // 秒
		lead     int
		weekdays []int
		wantH    int
		wantM    int
		wantDays []int
	}{
		{"整点无提前", 12 * 3600, 0, workdays, 12, 0, workdays},
		{"提前 5 秒要退到上一分钟", 12 * 3600, 5, workdays, 11, 59, workdays},
		{"提前 60 秒正好退一分钟", 12 * 3600, 60, workdays, 11, 59, workdays},
		{"非整点向下取整", 12*3600 + 30*60 + 40, 5, workdays, 12, 30, workdays},
		// 这条是真会咬人的：午夜前后提前，星期要跟着退一天
		{"跨午夜要把星期往前移", 2, 5, []int{1}, 23, 59, []int{0}},
		{"周日跨午夜回到周六", 0, 10, []int{0}, 23, 59, []int{6}},
		{"周日用 7 表示也认", 12 * 3600, 0, []int{7}, 12, 0, []int{0}},
		{"重复的星期要去重", 9 * 3600, 0, []int{1, 1, 8}, 9, 0, []int{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeTrigger(c.at, c.lead, c.weekdays)
			if got.Hour != c.wantH || got.Minute != c.wantM {
				t.Errorf("时刻 = %02d:%02d，想要 %02d:%02d", got.Hour, got.Minute, c.wantH, c.wantM)
			}
			if !reflect.DeepEqual(got.Weekdays, c.wantDays) {
				t.Errorf("星期 = %v，想要 %v", got.Weekdays, c.wantDays)
			}
		})
	}
}

// sched 创建一条测试定时。
func sched(label, at string, days []int) config.Schedule {
	return config.Schedule{Label: label, At: at, Weekdays: days,
		Theme: "default", Enabled: true, Grace: config.DefaultGrace}
}

// 2026-07-27 是周一
func mon(h, m, s int) time.Time {
	return time.Date(2026, 7, 27, h, m, s, 0, time.Local)
}

func TestMatchTargetUsesLatestPastTrigger(t *testing.T) {
	workdays := []int{1, 2, 3, 4, 5}
	c := &config.Config{Schedules: []config.Schedule{
		sched("noon", "12:00:00", workdays),
		sched("evening", "18:00:00", workdays),
	}}

	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"正点在中午", mon(12, 0, 0), "noon"},
		{"中午晚了两秒", mon(12, 0, 2), "noon"},
		{"正点在傍晚", mon(18, 0, 0), "evening"},
		{"下午不会提前匹配傍晚", mon(16, 30, 0), "noon"},
		{"深夜补跑仍能给出一条", mon(23, 50, 0), "evening"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := MatchTarget(c, tc.now)
			if result == nil {
				t.Fatal("没匹配到任何定时")
			}
			got := result.Schedule
			if got == nil {
				t.Fatal("没匹配到任何定时")
			}
			if got.Label != tc.want {
				t.Errorf("匹配到 %s，想要 %s", got.Label, tc.want)
			}
		})
	}
}

func TestMatchSkipsDisabled(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{
		sched("noon", "12:00:00", []int{1}),
		sched("evening", "18:00:00", []int{1}),
	}}
	c.Schedules[0].Enabled = false
	result := MatchTarget(c, mon(12, 0, 0))
	if result == nil {
		t.Fatal("没匹配到任何定时")
	}
	got := result.Schedule
	if got == nil || got.Label != "evening" {
		t.Fatalf("停用的定时不该被匹配到，拿到 %v", got)
	}
}

func TestMatchNoneEnabled(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{sched("noon", "12:00:00", []int{1})}}
	c.Schedules[0].Enabled = false
	if got := MatchTarget(c, mon(12, 0, 0)); got != nil {
		t.Fatalf("一条启用的都没有时应该返回 nil，拿到 %v", got)
	}
}

// withLeadTheme 创建不依赖仓库内置资源的测试主题。
func withLeadTheme(t *testing.T, lead int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "gong", "themes", "faketheme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	toml := fmt.Sprintf("lead = %d\n", lead)
	if err := os.WriteFile(filepath.Join(dir, "theme.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return "faketheme"
}

// 周日午夜前触发的定时，星期已经被移到周六，反查时也得对得上
func TestMatchAcrossMidnight(t *testing.T) {
	s := sched("mid", "00:00:02", []int{1})
	s.Theme = withLeadTheme(t, 5) // lead=5：周一目标会在周日 23:59 被拉起
	c := &config.Config{Schedules: []config.Schedule{s}}
	// 触发点在周日，反查结果仍必须指向周一的目标日期。
	now := time.Date(2026, 7, 26, 23, 59, 0, 0, time.Local)
	got := MatchTarget(c, now)
	if got == nil || got.Schedule.Label != "mid" {
		t.Fatalf("跨午夜的定时没匹配上，拿到 %v", got)
	}
	want := time.Date(2026, 7, 27, 0, 0, 2, 0, time.Local)
	if !got.Target.Equal(want) {
		t.Fatalf("目标时刻 = %s，想要 %s", got.Target, want)
	}

	// 到了午夜后再执行（例如 launchd 有一点延迟）也不能退回周日。
	got = MatchTarget(c, time.Date(2026, 7, 27, 0, 0, 1, 0, time.Local))
	if got == nil || !got.Target.Equal(want) {
		t.Fatalf("午夜后的目标时刻 = %v，想要 %s", got, want)
	}
}

func TestPlistMergesAllSchedules(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{
		sched("noon", "12:00:00", []int{1, 2}),
		sched("evening", "18:00:00", []int{1}),
	}}
	out := string(Plist("/opt/homebrew/bin/gong", c.Schedules))

	// 只能有一个 job
	if n := strings.Count(out, "<key>Label</key>"); n != 1 {
		t.Errorf("Label 出现 %d 次，应该只有 1 次", n)
	}
	if !strings.Contains(out, "<string>local.gong</string>") {
		t.Error("label 应该是 local.gong，不带定时名")
	}
	// 三个触发点：周一12点、周二12点、周一18点
	if n := strings.Count(out, "<key>Weekday</key>"); n != 3 {
		t.Errorf("触发点 %d 个，应该 3 个", n)
	}
	// fire 不带名字
	if strings.Contains(out, "<string>noon</string>") {
		t.Error("ProgramArguments 不该带定时名")
	}
	// 登录时绝不能跑
	if !strings.Contains(out, "<key>RunAtLoad</key>\n  <false/>") {
		t.Error("RunAtLoad 必须是 false")
	}
}

// 两条定时撞在同一分钟时只能留一个触发点，否则 launchd 叫醒两次、叠两个浮层
func TestPlistDedupesSameMinute(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{
		sched("a", "12:00:00", []int{1}),
		sched("b", "12:00:30", []int{1}),
	}}
	out := string(Plist("/opt/homebrew/bin/gong", c.Schedules))
	if n := strings.Count(out, "<key>Weekday</key>"); n != 1 {
		t.Errorf("同一分钟的触发点 %d 个，应该去重成 1 个", n)
	}
}

func TestValidateScheduleSetRejectsSameTriggerSlot(t *testing.T) {
	themeID := withLeadTheme(t, 0)
	c := &config.Config{Version: config.CurrentVersion, Schedules: []config.Schedule{
		{Label: "a", At: "12:00:00", Weekdays: []int{1}, Theme: themeID, Enabled: true, Grace: 10},
		{Label: "b", At: "12:00:30", Weekdays: []int{1}, Theme: themeID, Enabled: true, Grace: 10},
	}}
	err := ValidateScheduleSet(c)
	if err == nil || !strings.Contains(err.Error(), "触发时间冲突") {
		t.Fatalf("ValidateScheduleSet() error = %v, want trigger conflict", err)
	}
}

func TestValidateScheduleSetReportsMissingThemeBeforeSlotConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &config.Config{Version: config.CurrentVersion, Schedules: []config.Schedule{
		{Label: "a", At: "12:00:00", Weekdays: []int{1}, Theme: "missing-theme", Enabled: true, Grace: 10},
		{Label: "b", At: "12:00:30", Weekdays: []int{1}, Theme: "missing-theme", Enabled: true, Grace: 10},
	}}
	err := ValidateScheduleSet(c)
	if err == nil || !strings.Contains(err.Error(), "主题") || strings.Contains(err.Error(), "触发时间冲突") {
		t.Fatalf("ValidateScheduleSet() error = %v, want missing theme error", err)
	}
}

func TestMatchTargetDoesNotChooseFutureScheduleAfterDelay(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{
		sched("current", "12:00:00", []int{1}),
		sched("next", "12:01:00", []int{1}),
	}}
	got := MatchTarget(c, mon(12, 0, 40))
	if got == nil || got.Schedule.Label != "current" {
		t.Fatalf("延迟启动匹配到 %v，想要 current", got)
	}
}

func TestPlistSkipsDisabled(t *testing.T) {
	c := &config.Config{Schedules: []config.Schedule{
		sched("on", "12:00:00", []int{1}),
		sched("off", "18:00:00", []int{1}),
	}}
	c.Schedules[1].Enabled = false
	out := string(Plist("/opt/homebrew/bin/gong", c.Schedules))
	if n := strings.Count(out, "<key>Weekday</key>"); n != 1 {
		t.Errorf("停用的定时不该进 plist，触发点有 %d 个", n)
	}
}

// 新的 local.gong.plist 自己也长得像 local.gong.<name>.plist，
// 清理旧文件时绝不能把它当成遗留物删掉。
func TestLegacyScanIgnoresCurrentPlist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	agents := filepath.Join(dir, "Library", "LaunchAgents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"local.gong.plist", "local.gong.noon.plist", "local.gong.evening.plist"} {
		if err := os.WriteFile(filepath.Join(agents, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := legacyInstalled()
	want := []string{"evening", "noon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("扫到 %v，应该只有 %v（不含当前那份 local.gong.plist）", got, want)
	}
}

func TestWritePlistReplacesFileWithoutLeavingTemp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Dir(paths.PlistFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePlist([]byte("new plist")); err != nil {
		t.Fatalf("writePlist() error = %v", err)
	}
	got, err := os.ReadFile(paths.PlistFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new plist" {
		t.Fatalf("plist = %q, want %q", got, "new plist")
	}
	entries, err := os.ReadDir(filepath.Dir(paths.PlistFile()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".local.gong.plist.tmp-") {
			t.Errorf("temporary plist left behind: %s", entry.Name())
		}
	}
}

func TestSyncRejectsInvalidConfigBeforeWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &config.Config{Version: config.CurrentVersion, Schedules: []config.Schedule{{
		At: "25:00", Weekdays: []int{1}, Theme: "default", Enabled: true,
	}}}
	res := Sync(c, "/tmp/gong")
	if len(res.Errors) != 1 {
		t.Fatalf("Sync() errors = %v, want one validation error", res.Errors)
	}
	if _, err := os.Stat(paths.PlistFile()); !os.IsNotExist(err) {
		t.Fatalf("invalid config wrote a plist: %v", err)
	}
}

func TestSyncDoesNotMutateLaunchAgentsWhenThemeValidationFails(t *testing.T) {
	themeID := withLeadTheme(t, 0)
	if err := os.MkdirAll(paths.LaunchAgents(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PlistFile(), []byte("existing plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := paths.LegacyPlistFile("old")
	if err := os.WriteFile(legacy, []byte("legacy plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{Version: config.CurrentVersion, Schedules: []config.Schedule{
		{At: "12:00", Weekdays: []int{1}, Theme: themeID, Enabled: true},
		{At: "18:00", Weekdays: []int{1}, Theme: "missing-theme", Enabled: true},
	}}

	res := Sync(c, "/tmp/gong")
	if len(res.Errors) != 1 {
		t.Fatalf("Sync() errors = %v, want one theme error", res.Errors)
	}
	got, err := os.ReadFile(paths.PlistFile())
	if err != nil || string(got) != "existing plist" {
		t.Fatalf("current plist changed after validation failure: %q, %v", got, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy plist removed before validation completed: %v", err)
	}
}
