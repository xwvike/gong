// Package agent 负责 launchd 那一半：生成 plist、接管、卸载。
//
// 为什么不是 brew 帮我们装 plist：formula 的 install 跑在沙箱里只准写 Cellar 前缀，
// 而 brew services 一个 formula 只能有一个 service、cron 只接受一个表达式，
// 表达不了「12:00 + 18:00 + 用户任意增删」。所以这活儿必须自己干。
package agent

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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
	t := atSeconds - lead
	dayShift := 0
	for t < 0 { // 跨回前一天：00:00:02 提前 5 秒是昨天 23:59:57
		t += 86400
		dayShift--
	}
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

// Plist 生成一条定时的 launchd 配置。
//
// ProgramArguments 走 `gong fire <name>` 而不是直接调 gong-overlay：
// 收摊动作（暂停音乐、切专注模式）将来要在这一步跑，而且这样改主题不必重写 plist。
func Plist(gongPath string, s config.Schedule, tr Trigger) ([]byte, error) {
	var cal strings.Builder
	for _, d := range tr.Weekdays {
		fmt.Fprintf(&cal,
			"    <dict><key>Weekday</key><integer>%d</integer>"+
				"<key>Hour</key><integer>%d</integer>"+
				"<key>Minute</key><integer>%d</integer></dict>\n",
			d, tr.Hour, tr.Minute)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + esc(paths.Label(s.Name)) + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range []string{gongPath, "fire", s.Name} {
		b.WriteString("    <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>StartCalendarInterval</key>\n  <array>\n")
	b.WriteString(cal.String())
	b.WriteString("  </array>\n")
	// RunAtLoad 必须是 false：装的时候不该立刻放一遍
	b.WriteString("  <key>RunAtLoad</key>\n  <false/>\n")
	b.WriteString("  <key>StandardErrorPath</key>\n  <string>" + esc(paths.LogFile(s.Name)) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String()), nil
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ---- launchctl ----

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func run(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Bootout 卸载一条定时。已经不在了也算成功——这是幂等的。
func Bootout(name string) error {
	out, err := run("bootout", domain()+"/"+paths.Label(name))
	if err != nil {
		low := strings.ToLower(out)
		// 没加载过的 label 会报 "No such process" / 3，不算错
		if strings.Contains(low, "no such process") || strings.Contains(low, "could not find") {
			return nil
		}
		return fmt.Errorf("launchctl bootout %s: %s", name, out)
	}
	return nil
}

// Bootstrap 接管一条定时。先 bootout 再 bootstrap，
// 否则对已加载的 label 会报 "Bootstrap failed: 5: Input/output error"。
func Bootstrap(name string) error {
	_ = Bootout(name)
	out, err := run("bootstrap", domain(), paths.PlistFile(name))
	if err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %s", name, out)
	}
	return nil
}

// Loaded 报告这条定时现在是不是真的在 launchd 手里。
// 单独查一遍是因为「配置里 enabled」和「系统里真的装了」是两回事。
func Loaded(name string) bool {
	_, err := run("print", domain()+"/"+paths.Label(name))
	return err == nil
}

// Kickstart 立刻触发一次，调试用。
func Kickstart(name string) error {
	out, err := run("kickstart", "-k", domain()+"/"+paths.Label(name))
	if err != nil {
		return fmt.Errorf("launchctl kickstart %s: %s", name, out)
	}
	return nil
}

// ---- 同步 ----

type SyncResult struct {
	Installed []string
	Removed   []string
	Errors    []error
}

// Sync 把磁盘上的 plist 对齐到配置：enabled 的装上，disabled 和已删除的卸掉。
func Sync(c *config.Config, gongPath string) SyncResult {
	var res SyncResult

	if err := os.MkdirAll(paths.LaunchAgents(), 0o755); err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}

	want := map[string]bool{}
	for _, s := range c.Schedules {
		if s.Enabled {
			want[s.Name] = true
		}
	}

	// 先清掉不该在的：改名、删除、停用都走这条
	for _, name := range Installed() {
		if want[name] {
			continue
		}
		if err := Bootout(name); err != nil {
			res.Errors = append(res.Errors, err)
		}
		if err := os.Remove(paths.PlistFile(name)); err != nil && !os.IsNotExist(err) {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Removed = append(res.Removed, name)
	}

	for _, s := range c.Schedules {
		if !s.Enabled {
			continue
		}
		th, err := theme.Resolve(s.Theme)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("定时 %s：%w", s.Name, err))
			continue
		}
		tr := ComputeTrigger(s.Seconds(), th.LeadSeconds(), s.Weekdays)
		data, err := Plist(gongPath, s, tr)
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if err := os.WriteFile(paths.PlistFile(s.Name), data, 0o644); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if err := Bootstrap(s.Name); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Installed = append(res.Installed, s.Name)
	}
	return res
}

// Installed 列出磁盘上所有 local.gong.* 的定时名。
func Installed() []string {
	entries, err := os.ReadDir(paths.LaunchAgents())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "local.gong.") || !strings.HasSuffix(n, ".plist") {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(n, "local.gong."), ".plist"))
	}
	sort.Strings(names)
	return names
}

// RemoveAll 是 gong off：把所有 gong 的 plist 卸干净。
// brew uninstall 不会替我们做这件事，formula 没有 uninstall hook。
func RemoveAll() (removed []string, errs []error) {
	for _, name := range Installed() {
		if err := Bootout(name); err != nil {
			errs = append(errs, err)
		}
		if err := os.Remove(paths.PlistFile(name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, name)
	}
	return
}

// PlistPathFor 给 ls 显示用
func PlistPathFor(name string) string { return filepath.Clean(paths.PlistFile(name)) }
