// Package agent 负责 launchd 那一半：生成 plist、接管、卸载。
//
// 为什么不是 brew 帮我们装 plist：formula 的 install 跑在沙箱里只准写 Cellar 前缀，
// 而 brew services 一个 formula 只能有一个 service、cron 只接受一个表达式，
// 表达不了「12:00 + 18:00 + 用户任意增删」。所以这活儿必须自己干。
//
// 所有定时共用【一个】 plist。`StartCalendarInterval` 本来就是个数组，
// 装多少个「星期几 + 几点几分」都行，没必要一条定时一个 job。
package agent

import (
	"encoding/xml"
	"fmt"
	"math"
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

// Trigger 是 launchd 实际被叫醒的时刻。
//
// StartCalendarInterval 只有 Hour/Minute，【没有 Second】。所以 lead=5 的定时
// 不可能让 launchd 在 11:59:55 触发，只能落到 11:59:00，剩下的 55 秒由壳自己等
// （壳会先把页面加载好但不亮相）。
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

// Occurrences 给出这个触发点在 now 前后一天内的所有绝对时刻，用来反查是哪条定时。
func (t Trigger) Occurrences(now time.Time) []time.Time {
	var out []time.Time
	for _, off := range []int{-1, 0, 1} {
		d := now.AddDate(0, 0, off)
		wd := int(d.Weekday()) // Go 的 Sunday=0..Saturday=6，跟 launchd 口径一致
		for _, w := range t.Weekdays {
			if w == wd {
				out = append(out, time.Date(d.Year(), d.Month(), d.Day(),
					t.Hour, t.Minute, 0, 0, d.Location()))
				break
			}
		}
	}
	return out
}

// TriggerFor 算一条定时的触发点，主题找不到时按 lead=0 处理。
func TriggerFor(s config.Schedule) Trigger {
	return ComputeTrigger(s.Seconds(), scheduleLead(s), s.Weekdays)
}

// scheduleLead 返回生成 launchd 触发点时使用的提前秒数。
//
// 主题不存在时跟 TriggerFor 一样按 lead=0 处理；真正执行时 cmdFire
// 还会再次解析主题并报错。保持这里的回退行为可以让反查在配置暂时损坏时
// 仍然是确定的，不会因为反查本身 panic。
func scheduleLead(s config.Schedule) int {
	if th, err := theme.Resolve(s.Theme); err == nil {
		return th.LeadSeconds()
	}
	return 0
}

// targetForTrigger 把 launchd 的触发点还原成定时真正的目标时刻。
//
// StartCalendarInterval 只能精确到分钟，而提前亮相还可能把触发点推到
// 前一天。仅把 at 的时分秒套到「今天」会在这种情况下得到错误日期（例如
// 周一 00:00:02、lead=5 会在周日 23:59 被拉起）。这里复用 ComputeTrigger
// 的日偏移规则，从触发点的日期还原目标日期，再写入原始秒数。
func targetForTrigger(s config.Schedule, trigger time.Time) time.Time {
	at := s.Seconds()
	_, dayShift := triggerTimeOfDay(at, scheduleLead(s))
	day := trigger.AddDate(0, 0, -dayShift)
	return time.Date(day.Year(), day.Month(), day.Day(),
		at/3600, (at/60)%60, at%60, 0, day.Location())
}

// nearestTrigger 找出一个定时离 now 最近的 launchd 触发点。
func nearestTrigger(t Trigger, now time.Time) (time.Time, bool) {
	var best time.Time
	bestDelta := time.Duration(math.MaxInt64)
	for _, cand := range t.Occurrences(now) {
		d := now.Sub(cand)
		if d < 0 {
			d = -d
		}
		if d < bestDelta {
			best, bestDelta = cand, d
		}
	}
	return best, !best.IsZero()
}

// MatchResult 是合并 plist 后的一次反查结果。launchd 不会告诉我们是哪条
// 定时触发，所以除了 schedule 还要保留它对应的绝对目标时刻，交给 Swift
// 壳使用；否则跨午夜时壳会把目标错误解析到触发日。
type MatchResult struct {
	Schedule *config.Schedule
	Index    int // 在 c.Schedules 里的位置，打日志标签时当「#N」用，不依赖名字
	Target   time.Time
}

// MatchTarget 反查「现在这一下是哪条定时」以及它的绝对目标时刻。
func MatchTarget(c *config.Config, now time.Time) *MatchResult {
	best, bestDelta := -1, time.Duration(math.MaxInt64)
	var bestTrigger time.Time
	for i := range c.Schedules {
		if !c.Schedules[i].Enabled {
			continue
		}
		trigger, ok := nearestTrigger(TriggerFor(c.Schedules[i]), now)
		if !ok {
			continue
		}
		d := now.Sub(trigger)
		if d < 0 {
			d = -d
		}
		if d < bestDelta {
			best, bestDelta, bestTrigger = i, d, trigger
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

// TargetFor 返回一条定时离 now 最近的目标时刻，供 cmdFire 补出绝对日期。
func TargetFor(s config.Schedule, now time.Time) time.Time {
	trigger, ok := nearestTrigger(TriggerFor(s), now)
	if !ok {
		return time.Time{}
	}
	return targetForTrigger(s, trigger)
}

// Match 反查「现在这一下是哪条定时」。
//
// 合并成一个 plist 之后 launchd 不再告诉我们是谁触发的，只能按时间反推：
// 取触发点离 now 最近的那条。猜错也不至于出事——壳自己还有时间窗判断会把
// 不该放的挡掉（比如睡醒后 launchd 补跑）。
func Match(c *config.Config, now time.Time) *config.Schedule {
	if result := MatchTarget(c, now); result != nil {
		return result.Schedule
	}
	return nil
}

// Plist 生成那唯一一个 launchd 配置，把所有启用定时的触发点并进去。
//
// ProgramArguments 走 `gong fire` 而不是直接调 gong-overlay：
// 收摊动作（暂停音乐、切专注模式）将来要在这一步跑，而且这样改主题不必重写 plist。
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

// Loaded 报告 launchd 现在是不是真的接管着。
// 单独查一遍是因为「配置里 enabled」和「系统里真的装了」是两回事——
// 用户还可能在系统设置的「登录项与扩展」里把它关掉。
func Loaded() bool {
	_, err := run("print", domain()+"/"+paths.Label)
	return err == nil
}

// Kickstart 立刻触发一次，调试用。
func Kickstart() error {
	out, err := run("kickstart", "-k", domain()+"/"+paths.Label)
	if err != nil {
		return fmt.Errorf("launchctl kickstart: %s", out)
	}
	return nil
}

// ---- 同步 ----

// ActiveSchedule 是一条被装进 plist 的定时，连它在 c.Schedules 里的
// 真实位置一起带出来——Active 只收启用且主题有效的子集，下标从 0 重新数，
// 跟原始位置对不上，所以不能只传 config.Schedule，DisplayName(i) 会算错。
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
	if err := c.Validate(); err != nil {
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
		if _, err := theme.Resolve(s.Theme); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("定时 %s：%w", s.DisplayName(i), err))
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
