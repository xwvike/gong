// Package agent 管理包含全部定时的单个 launchd plist。
package agent

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
	"github.com/xwvike/gong/internal/theme"
)

// Trigger 是 launchd 可表达的分钟级触发时刻。
type Trigger struct {
	Hour, Minute int
	Weekdays     []int
}

// ComputeTrigger 把「几点几分几秒 + 提前多少秒」换算成 launchd 能表达的触发点。
func ComputeTrigger(atSeconds, lead int, weekdays []int) Trigger {
	t, dayShift := triggerTimeOfDay(atSeconds, lead)
	minuteOfDay := t / 60 // 向下取整到分钟，宁可早一点也不能晚

	days := make([]int, 0, len(weekdays))
	seen := map[int]bool{}
	for _, d := range weekdays {
		d = ((d+dayShift)%7 + 7) % 7 // launchd 里 0 和 7 都是周日，统一成 0..6
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Ints(days)
	return Trigger{Hour: minuteOfDay / 60, Minute: minuteOfDay % 60, Weekdays: days}
}

func triggerTimeOfDay(atSeconds, lead int) (seconds, dayShift int) {
	seconds = atSeconds - lead
	for seconds < 0 { // 跨回前一天：00:00:02 提前 5 秒是昨天 23:59:57
		seconds += 86400
		dayShift--
	}
	return seconds, dayShift
}

// latestOccurrence 返回不晚于 now 的最近触发点。launchd 不会提前启动任务，
// 因此不能用绝对距离匹配未来的定时。
func (t Trigger) latestOccurrence(now time.Time) (time.Time, bool) {
	var best time.Time
	for off := 0; off >= -7; off-- {
		d := now.AddDate(0, 0, off)
		wd := int(d.Weekday())
		for _, w := range t.Weekdays {
			if w != wd {
				continue
			}
			candidate := time.Date(d.Year(), d.Month(), d.Day(),
				t.Hour, t.Minute, 0, 0, d.Location())
			if !candidate.After(now) && (best.IsZero() || candidate.After(best)) {
				best = candidate
			}
			break
		}
	}
	return best, !best.IsZero()
}

// TriggerFor 算一条定时的触发点，主题找不到时按 lead=0 处理。
func TriggerFor(s config.Schedule) Trigger {
	return ComputeTrigger(s.Seconds(), scheduleLead(s), s.Weekdays)
}

// scheduleLead 对动态策略取全部候选主题的最大值。
func scheduleLead(s config.Schedule) int {
	return theme.WakeLead(s.Theme)
}

// targetForTrigger 按相同的跨日偏移规则还原绝对目标时刻。
func targetForTrigger(s config.Schedule, trigger time.Time) time.Time {
	at := s.Seconds()
	_, dayShift := triggerTimeOfDay(at, scheduleLead(s))
	day := trigger.AddDate(0, 0, -dayShift)
	return time.Date(day.Year(), day.Month(), day.Day(),
		at/3600, (at/60)%60, at%60, 0, day.Location())
}

// MatchResult 保存共享 job 反查出的定时和绝对目标时刻。
type MatchResult struct {
	Schedule *config.Schedule
	Index    int // 在 c.Schedules 里的位置，打日志标签时当「#N」用，不依赖名字
	Target   time.Time
}

// MatchTarget 反查「现在这一下是哪条定时」以及它的绝对目标时刻。
func MatchTarget(c *config.Config, now time.Time) *MatchResult {
	best := -1
	var bestTrigger time.Time
	for i := range c.Schedules {
		if !c.Schedules[i].Enabled {
			continue
		}
		trigger, ok := TriggerFor(c.Schedules[i]).latestOccurrence(now)
		if !ok {
			continue
		}
		if best < 0 || trigger.After(bestTrigger) {
			best, bestTrigger = i, trigger
		}
	}
	if best < 0 {
		return nil
	}
	return &MatchResult{
		Schedule: &c.Schedules[best],
		Index:    best,
		Target:   targetForTrigger(c.Schedules[best], bestTrigger),
	}
}

type triggerSlot struct{ weekday, hour, minute int }

// ValidateTriggerSlots 拒绝会落入同一个 launchd 分钟槽的启用定时。
func ValidateTriggerSlots(c *config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return validateTriggerSlots(c)
}

func validateTriggerSlots(c *config.Config) error {
	owners := make(map[triggerSlot]int)
	for i, s := range c.Schedules {
		if !s.Enabled {
			continue
		}
		tr := TriggerFor(s)
		for _, weekday := range tr.Weekdays {
			slot := triggerSlot{weekday, tr.Hour, tr.Minute}
			if previous, exists := owners[slot]; exists {
				return fmt.Errorf("定时 %s 与 %s 的 launchd 触发时间冲突（星期 %d %02d:%02d）",
					c.Schedules[previous].Ref(previous), s.Ref(i), weekday, tr.Hour, tr.Minute)
			}
			owners[slot] = i
		}
	}
	return nil
}

// ValidateScheduleSet 在写 plist 前完成资源和触发槽校验。
func ValidateScheduleSet(c *config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	for i, s := range c.Schedules {
		if !s.Enabled {
			continue
		}
		if err := theme.ValidateChoice(s.Theme); err != nil {
			return fmt.Errorf("定时 %s：%w", s.Ref(i), err)
		}
	}
	return validateTriggerSlots(c)
}

// Plist 把所有启用定时合并成一个 launchd 配置。
func Plist(gongPath string, schedules []config.Schedule) []byte {
	// 去重：两条定时撞在同一分钟时只留一个触发点，
	// 否则 launchd 会叫醒两次，屏幕上叠两个浮层。
	type slot struct{ w, h, m int }
	seen := map[slot]bool{}
	var slots []slot
	for _, s := range schedules {
		if !s.Enabled {
			continue
		}
		tr := TriggerFor(s)
		for _, w := range tr.Weekdays {
			k := slot{w, tr.Hour, tr.Minute}
			if !seen[k] {
				seen[k] = true
				slots = append(slots, k)
			}
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		a, b := slots[i], slots[j]
		if a.w != b.w {
			return a.w < b.w
		}
		if a.h != b.h {
			return a.h < b.h
		}
		return a.m < b.m
	})

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + esc(paths.Label) + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range []string{gongPath, "fire"} {
		b.WriteString("    <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>StartCalendarInterval</key>\n  <array>\n")
	for _, s := range slots {
		fmt.Fprintf(&b,
			"    <dict><key>Weekday</key><integer>%d</integer>"+
				"<key>Hour</key><integer>%d</integer>"+
				"<key>Minute</key><integer>%d</integer></dict>\n",
			s.w, s.h, s.m)
	}
	b.WriteString("  </array>\n")
	// RunAtLoad 必须是 false：装的时候不该立刻放一遍，登录时也不该跑任何东西
	b.WriteString("  <key>RunAtLoad</key>\n  <false/>\n")
	b.WriteString("  <key>StandardErrorPath</key>\n  <string>" + esc(paths.LogFile()) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ---- launchctl ----

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func run(args ...string) (string, error) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// bootoutLabel 卸载一个 label。已经不在了也算成功——这是幂等的。
func bootoutLabel(label string) error {
	out, err := run("bootout", domain()+"/"+label)
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "no such process") || strings.Contains(low, "could not find") {
			return nil
		}
		return fmt.Errorf("launchctl bootout %s: %s", label, out)
	}
	return nil
}

func Bootout() error { return bootoutLabel(paths.Label) }

// Bootstrap 接管。先 bootout 再 bootstrap，
// 否则对已加载的 label 会报 "Bootstrap failed: 5: Input/output error"。
func Bootstrap() error {
	if err := Bootout(); err != nil {
		return err
	}
	out, err := run("bootstrap", domain(), paths.PlistFile())
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %s", out)
	}
	return nil
}

// Loaded 报告 launchd 是否已加载当前 job。
func Loaded() bool {
	_, err := run("print", domain()+"/"+paths.Label)
	return err == nil
}

// ---- 同步 ----

// ActiveSchedule 保留定时在原配置中的序号，供安装摘要使用。
type ActiveSchedule struct {
	Index    int
	Schedule config.Schedule
}

type SyncResult struct {
	Active  []ActiveSchedule // 这次写进 plist 的定时，用于打印摘要
	Cleaned []string         // 清掉的老式 per-name plist
	Removed bool             // 一条启用的都没有，plist 被整个撤了
	Errors  []error
}

// Sync 把磁盘上的 plist 对齐到配置。幂等，可以随便重复跑。
func Sync(c *config.Config, gongPath string) SyncResult {
	var res SyncResult
	if err := ValidateScheduleSet(c); err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}

	if err := os.MkdirAll(paths.LaunchAgents(), 0o755); err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}

	// 0.1.0 是一条定时一个 plist，升上来的时候要把它们清掉，
	// 否则系统设置的后台项里会一直挂着几个重复的 gong。
	for _, name := range legacyInstalled() {
		if err := bootoutLabel(paths.LegacyLabel(name)); err != nil {
			res.Errors = append(res.Errors, err)
		}
		if err := os.Remove(paths.LegacyPlistFile(name)); err != nil && !os.IsNotExist(err) {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Cleaned = append(res.Cleaned, name)
	}

	var valid []config.Schedule
	for i, s := range c.Schedules {
		if !s.Enabled {
			continue
		}
		res.Active = append(res.Active, ActiveSchedule{Index: i, Schedule: s})
		valid = append(valid, s)
	}

	if len(res.Active) == 0 {
		// 一条都没有就别在系统里留个空 job
		if err := Bootout(); err != nil {
			res.Errors = append(res.Errors, err)
		}
		if err := os.Remove(paths.PlistFile()); err != nil && !os.IsNotExist(err) {
			res.Errors = append(res.Errors, err)
		}
		res.Removed = true
		return res
	}

	if err := writePlist(Plist(gongPath, valid)); err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	if err := Bootstrap(); err != nil {
		res.Errors = append(res.Errors, err)
	}
	return res
}

// writePlist 用同目录临时文件再 rename，避免 launchd 在写入中途读到半份 XML。
func writePlist(data []byte) error {
	path := paths.PlistFile()
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

// legacyInstalled 找出 0.1.0 留下的 local.gong.<name>.plist。
func legacyInstalled() []string {
	entries, err := os.ReadDir(paths.LaunchAgents())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		// 当心：新的 local.gong.plist 自己也符合 local.gong.*.plist 这个形状，
		// 不排掉的话清理老文件时会把刚写好的那份删掉。
		if n == paths.Label+".plist" {
			continue
		}
		if !strings.HasPrefix(n, paths.Label+".") || !strings.HasSuffix(n, ".plist") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(n, paths.Label+"."), ".plist")
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RemoveAll 是 gong off：把 gong 在 launchd 里的痕迹清干净，含 0.1.0 的老 plist。
// brew uninstall 不会替我们做这件事，formula 没有 uninstall hook。
func RemoveAll() (removed []string, errs []error) {
	for _, name := range legacyInstalled() {
		if err := bootoutLabel(paths.LegacyLabel(name)); err != nil {
			errs = append(errs, err)
		}
		if err := os.Remove(paths.LegacyPlistFile(name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, "local.gong."+name+"（旧版）")
	}
	if err := Bootout(); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(paths.PlistFile()); err == nil {
		removed = append(removed, paths.Label)
	} else if !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	return
}
