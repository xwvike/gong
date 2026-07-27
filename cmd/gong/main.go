// gong —— 定时全屏浮层提醒。
//
// 分层：
//
//	壳 (gong-overlay)  计时、拉起主题、给主题提供能力
//	Go (gong)          配置、TUI、launchd 接管
//	主题                纯 HTML/CSS，表现力在设计上
//
// 常驻进程数为 0：launchd 本来就在跑，到点拉起 gong fire，播完自杀。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xwvike/gong/internal/agent"
	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
	"github.com/xwvike/gong/internal/theme"
	"github.com/xwvike/gong/internal/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return
	}
	var err error
	switch args[0] {
	case "on":
		err = cmdOn()
	case "off":
		err = cmdOff()
	case "set":
		err = cmdSet()
	case "ls":
		err = cmdLs()
	case "rm":
		err = cmdRm(args[1:])
	case "vis":
		err = cmdVis(args[1:])
	case "stop":
		err = cmdStop()
	case "fire":
		err = cmdFire(args[1:])
	case "themes":
		err = cmdThemes()
	case "version", "--version", "-v":
		fmt.Println("gong", paths.Version)
	case "help", "--help", "-h":
		usage()
	default:
		err = fmt.Errorf("不认识的子命令 %q，跑 gong help 看看", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gong:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`gong —— 到点在所有屏幕最顶层播一段动画，不抢焦点、不吃点击

  gong on            写默认配置并接管 launchd（装完跑这一条就能用）
  gong off           卸掉所有定时（brew uninstall 之前跑这条）
  gong set           TUI：增删改查定时、选主题、预览
  gong ls            列出定时和它们的实际状态
  gong rm <name>     删掉一条定时
  gong vis <theme>   预览一个主题
  gong stop          掐掉正在播的浮层
  gong themes        列出可用主题

默认两条定时：noon 12:00、evening 18:00，周一到周五。
`)
}

// selfPath 是要写进 plist 的路径。必须是稳定的 opt 路径，
// 不能是 Cellar 里带版本号的那个——brew upgrade 之后它就没了。
func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return paths.StablePath(abs), nil
}

func cmdOn() error {
	// 已经有配置就照装，不覆盖用户改过的东西
	if config.Exists() {
		c, err := config.Load()
		if err != nil {
			return err
		}
		return install(c)
	}
	c := config.Default()
	if err := c.Validate(); err != nil {
		return err
	}
	if err := c.Save(); err != nil {
		return err
	}
	if err := config.EnsureUserThemeDir(); err != nil {
		return err
	}
	fmt.Println("已写入", paths.ConfigFile())
	return install(c)
}

func install(c *config.Config) error {
	self, err := selfPath()
	if err != nil {
		return err
	}
	if paths.Overlay() == "" {
		return fmt.Errorf("找不到 gong-overlay（找过 %s 和可执行文件同级目录）", paths.OverlayPath)
	}
	res := agent.Sync(c, self)
	for _, n := range res.Cleaned {
		fmt.Println("已清掉旧版遗留的 plist:", n)
	}
	for _, n := range res.Active {
		s, _ := c.Find(n)
		tr := agent.TriggerFor(*s)
		fmt.Printf("%-10s %s %s  主题 %-8s (launchd 在 %02d:%02d 叫醒)\n",
			n, s.At, s.WeekdaysLabel(), s.Theme, tr.Hour, tr.Minute)
	}
	for _, e := range res.Errors {
		fmt.Fprintln(os.Stderr, "  !", e)
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("有 %d 条没装上", len(res.Errors))
	}
	if res.Removed {
		fmt.Println("没有启用中的定时，已从 launchd 撤出。跑 gong set 加一条。")
		return nil
	}
	// 所有定时共用一个 launchd job，所以系统设置的「登录项与扩展」里
	// 永远只有一个 gong，不管你配了几条。
	fmt.Printf("\n以上 %d 条已交给 launchd（%s，后台项只占一个）\n", len(res.Active), paths.Label)
	return nil
}

func cmdOff() error {
	removed, errs := agent.RemoveAll()
	for _, n := range removed {
		fmt.Println("已卸载", n)
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  !", e)
	}
	if len(removed) == 0 && len(errs) == 0 {
		fmt.Println("本来就没装。")
	}
	fmt.Println("配置留在", paths.ConfigFile(), "——再跑 gong on 就能装回来。")
	return nil
}

func cmdSet() error {
	c, err := config.LoadOrDefault()
	if err != nil {
		return err
	}
	changed, err := tui.Run(c)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("没有改动。")
		return nil
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Println("已保存", paths.ConfigFile())
	return install(c)
}

func cmdLs() error {
	c, err := config.LoadOrDefault()
	if err != nil {
		return err
	}
	if !config.Exists() {
		fmt.Print("还没有配置文件。跑 gong on 用默认的两条，或 gong set 自己编。\n\n")
	}
	if len(c.Schedules) == 0 {
		fmt.Println("一条定时都没有。")
		return nil
	}
	fmt.Printf("%-12s %-9s %-8s %-10s %-8s %s\n", "名字", "时间", "星期", "主题", "状态", "launchd 叫醒")
	for _, s := range c.Schedules {
		state := "停用"
		if s.Enabled {
			state = "启用"
		}
		wake := "—"
		if s.Enabled {
			tr := agent.TriggerFor(s)
			wake = fmt.Sprintf("%02d:%02d", tr.Hour, tr.Minute)
		}
		note := ""
		if _, err := theme.Resolve(s.Theme); err != nil {
			note = "  ← 主题找不到"
		}
		fmt.Printf("%-12s %-9s %-8s %-10s %-8s %s%s\n",
			s.Name, s.At, s.WeekdaysLabel(), s.Theme, state, wake, note)
	}

	// 所有定时共用一个 launchd job，所以接管状态是全局的一个。
	fmt.Println()
	if agent.Loaded() {
		fmt.Printf("launchd：已接管（%s，系统设置的后台项里只占一个）\n", paths.Label)
	} else {
		fmt.Println("launchd：未接管 —— 跑 gong on")
	}
	return nil
}

func cmdRm(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("要删哪条？gong rm <name>")
	}
	name := args[0]
	c, err := config.LoadOrDefault()
	if err != nil {
		return err
	}
	if !c.Remove(name) {
		return fmt.Errorf("没有叫 %q 的定时", name)
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Println("已删除", name)
	// 定时是共用一个 plist 的，删一条要重新生成整份
	return install(c)
}

func cmdThemes() error {
	list := theme.List()
	if len(list) == 0 {
		return fmt.Errorf("一个主题都没找到（找过 %s 和 %s）", paths.UserThemes(), paths.Builtin())
	}
	for _, t := range list {
		src := "内置"
		if !t.Builtin {
			src = "自定义"
		}
		fmt.Printf("%-10s %-6s lead %-3d %-8s %s\n", t.ID, src, t.LeadSeconds(), t.Meta.Placement, t.Meta.Desc)
	}
	return nil
}

// cmdVis 预览。走的是和真实触发【完全同一条渲染路径】，只是加了 --force
// 跳过时间窗和全屏判断。别为预览另写一套。
func cmdVis(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("要预览哪个主题？gong vis <theme>（gong themes 看有哪些）")
	}
	th, err := theme.Resolve(args[0])
	if err != nil {
		return err
	}
	overlay := paths.Overlay()
	if overlay == "" {
		return fmt.Errorf("找不到 gong-overlay")
	}
	cmd := exec.Command(overlay,
		"--force",
		"--lead", strconv.Itoa(th.LeadSeconds()),
		"--timeout", strconv.Itoa(th.TimeoutSeconds()),
		"--name", "vis",
		"--theme", th.HTML,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdStop() error {
	out, err := exec.Command("pkill", "-x", "gong-overlay").CombinedOutput()
	if err != nil {
		// pkill 没杀到任何进程时退出码是 1，这不是错误
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			fmt.Println("现在没有在播的浮层。")
			return nil
		}
		return fmt.Errorf("pkill: %s", strings.TrimSpace(string(out)))
	}
	fmt.Println("已掐掉。")
	return nil
}

// cmdFire 是 launchd 的入口。
//
// 这里将来要挂「收摊」动作（暂停音乐、shortcuts run 切专注、退 Xcode），
// 所以让 launchd 调 gong 而不是直接调 gong-overlay。
// 最后用 syscall.Exec 把自己换成壳，不留多余进程。
func cmdFire(args []string) error {
	c, err := config.Load()
	if err != nil {
		return err
	}

	var s *config.Schedule
	if len(args) > 0 {
		// 带名字的调法只为兼容 0.1.0 遗留的 per-name plist，
		// 那些 plist 会在下次 gong on 时被清掉。
		if s, _ = c.Find(args[0]); s == nil {
			return fmt.Errorf("配置里没有定时 %q", args[0])
		}
	} else {
		// 所有定时共用一个 job，launchd 不会告诉我们是谁触发的，按时间反查。
		// 猜错也不至于出事：壳自己的时间窗判断会把不该放的挡掉。
		s = agent.Match(c, time.Now())
		if s == nil {
			return nil // 一条启用的都没有，安静退出
		}
	}
	if !s.Enabled {
		return nil // 停用了就安静退出，不该是错误
	}
	th, err := theme.Resolve(s.Theme)
	if err != nil {
		return err
	}
	overlay := paths.Overlay()
	if overlay == "" {
		return fmt.Errorf("找不到 gong-overlay")
	}

	// TODO 收摊动作在这里跑，跑完再 exec 到壳

	argv := []string{overlay,
		"--at", config.FormatClock(s.Seconds()),
		"--lead", strconv.Itoa(th.LeadSeconds()),
		"--grace", strconv.Itoa(s.Grace),
		"--timeout", strconv.Itoa(th.TimeoutSeconds()),
		"--name", s.Name,
		"--theme", th.HTML,
	}
	return syscall.Exec(overlay, argv, os.Environ())
}
