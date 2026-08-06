// gong 管理全屏提醒的配置、主题和 launchd 调度。
package main

import (
	"bufio"
	"errors"
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
	"github.com/xwvike/gong/internal/player"
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
	case "uninstall":
		err = cmdUninstall(args[1:])
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
	fmt.Print(`gong —— I'm outta here!

  gong on            启用 gong，让它提醒你该溜了！
  gong set           进入定时器管理
  gong ls            列出现有定时
  gong rm <序号>     删掉一条定时
  gong vis <theme>   预览一个主题
  gong stop          掐掉正在播的浮层
  gong themes        列出可用主题

  gong off           关掉 gong，但保留配置和程序（想暂停一阵子用这个）
  gong uninstall     卸载 gong，并清掉 launchd 里的残留
      --purge        连 ~/.config/gong 一起删（包含自己写的主题）
      -y             不用确认

默认定时：#1 12:00（午间）、#2 18:00（下班），周一到周五。
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
	for _, a := range res.Active {
		s := a.Schedule
		tr := agent.TriggerFor(s)
		fmt.Printf("%-6s %s %s  主题 %-8s (launchd 在 %02d:%02d 叫醒)\n",
			s.Ref(a.Index), s.At, s.WeekdaysLabel(), config.ThemeLabel(s.Theme), tr.Hour, tr.Minute)
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
	if len(errs) > 0 {
		return fmt.Errorf("launchd 清理未完成：%w", errors.Join(errs...))
	}
	return nil
}

// cmdUninstall 同时清理 launchd；Homebrew formula 没有 uninstall hook。
func cmdUninstall(args []string) error {
	purge, yes := false, false
	for _, a := range args {
		switch a {
		case "--purge":
			purge = true
		case "-y", "--yes":
			yes = true
		default:
			return fmt.Errorf("不认识的参数 %q（gong uninstall [--purge] [-y]）", a)
		}
	}

	brewPath, formulaName := brewInstall()

	fmt.Println("将要：")
	fmt.Println("  · 掐掉正在播的浮层")
	fmt.Println("  · 从 launchd 撤出，删掉 ~/Library/LaunchAgents/local.gong*.plist")
	if purge {
		fmt.Printf("  · 删掉 %s（%s）\n", paths.ConfigDir(), userThemeNote())
	} else {
		fmt.Printf("  · 保留 %s（要一并删加 --purge）\n", paths.ConfigDir())
	}
	if brewPath != "" {
		fmt.Printf("  · 执行 brew uninstall %s\n", formulaName)
	} else {
		fmt.Println("  · 不是 brew 装的，程序本体要你自己删")
	}

	if !yes && !confirm("确定吗？[y/N] ") {
		fmt.Println("取消了，什么都没动。")
		return nil
	}

	// 正在播的话先掐掉，免得卸完还挂在屏幕上
	_ = exec.Command("pkill", "-x", "gong-overlay").Run()

	removed, errs := agent.RemoveAll()
	for _, n := range removed {
		fmt.Println("已卸载", n)
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  !", e)
	}
	if len(errs) > 0 {
		return fmt.Errorf("launchd 清理未完成，已中止卸载：%w", errors.Join(errs...))
	}

	if purge {
		if err := os.RemoveAll(paths.ConfigDir()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删配置失败：%w", err)
		}
		fmt.Println("已删除", paths.ConfigDir())
	}

	if brewPath == "" {
		fmt.Println("\nlaunchd 那边已经干净了。程序本体不是 brew 装的，自己删：")
		if p := paths.Overlay(); p != "" {
			fmt.Println("  ", p)
		}
		if exe, err := os.Executable(); err == nil {
			fmt.Println("  ", exe)
		}
		return nil
	}

	// 用 exec 换掉自己：进程映像已经变成 brew 了，
	// 它接下来删掉 /opt/homebrew/bin/gong 完全没有「删正在跑的文件」这个问题。
	fmt.Printf("\n$ brew uninstall %s\n", formulaName)
	return syscall.Exec(brewPath, []string{"brew", "uninstall", formulaName}, os.Environ())
}

// brewInstall 根据可执行文件位置判断当前 gong 是否由 Homebrew 安装。
func brewInstall() (brewPath, formula string) {
	brewPath, err := exec.LookPath("brew")
	if err != nil {
		return "", ""
	}
	out, err := exec.Command(brewPath, "--prefix").Output()
	if err != nil {
		return "", ""
	}
	prefix := strings.TrimSpace(string(out))
	exe, err := os.Executable()
	if err != nil {
		return "", ""
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	if !strings.HasPrefix(exe, prefix+string(filepath.Separator)) {
		return "", ""
	}
	// tap 里的 formula 全名，卸载时用短名就够
	return brewPath, "gong"
}

func userThemeNote() string {
	entries, err := os.ReadDir(paths.UserThemes())
	if err != nil {
		return "没有自定义主题"
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	if n == 0 {
		return "没有自定义主题"
	}
	return fmt.Sprintf("含 %d 个你自己写的主题！", n)
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false // 非交互环境（管道、launchd）一律当成否
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
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
	if err := agent.ValidateScheduleSet(c); err != nil {
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
	fmt.Printf("%-4s %-8s %-9s %-8s %-10s %-8s %s\n", "序号", "标签", "时间", "星期", "主题", "状态", "launchd 叫醒")
	for i, s := range c.Schedules {
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
		if err := theme.ValidateChoice(s.Theme); err != nil {
			note = "  ← 主题找不到"
		}
		label := s.Label
		if label == "" {
			label = "—"
		}
		fmt.Printf("#%-3d %-8s %-9s %-8s %-10s %-8s %s%s\n",
			i+1, label, s.At, s.WeekdaysLabel(), config.ThemeLabel(s.Theme), state, wake, note)
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

// cmdRm 删除前展示当前序号对应的定时，避免配置变化后误删。
func cmdRm(args []string) error {
	yes := false
	var numArg string
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			yes = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("不认识的参数 %q（gong rm <序号> [-y]）", a)
			}
			if numArg != "" {
				return fmt.Errorf("参数太多（gong rm <序号> [-y]）")
			}
			numArg = a
		}
	}
	if numArg == "" {
		return fmt.Errorf("要删哪条？gong rm <序号>（序号看 gong ls）")
	}
	n, err := strconv.Atoi(numArg)
	if err != nil {
		return fmt.Errorf("序号得是个数字，收到 %q", numArg)
	}
	c, err := config.LoadOrDefault()
	if err != nil {
		return err
	}
	s, ok := c.At(n - 1)
	if !ok {
		return fmt.Errorf("没有第 %d 条定时（跑 gong ls 看看现在有几条）", n)
	}

	fmt.Println(rmPreview(n, *s))
	if !yes && !confirm("确定吗？[y/N] ") {
		fmt.Println("取消了，什么都没动。")
		return nil
	}

	display := s.Ref(n - 1)
	if !c.RemoveAt(n - 1) {
		return fmt.Errorf("没有第 %d 条定时", n)
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Println("已删除", display)
	// 定时是共用一个 plist 的，删一条要重新生成整份
	return install(c)
}

// rmPreview 始终显示序号，仅在存在标签时追加标签。
func rmPreview(n int, s config.Schedule) string {
	line := fmt.Sprintf("即将删除：#%d %s %s  主题 %s", n, s.At, s.WeekdaysLabel(), config.ThemeLabel(s.Theme))
	if s.Label != "" {
		line += "  标签 " + s.Label
	}
	return line
}

func cmdThemes() error {
	list := theme.List()
	if len(list) == 0 {
		return fmt.Errorf("一个主题都没找到（找过 %s 和 %s）", paths.UserThemes(), paths.Builtin())
	}
	info, _ := os.Stdout.Stat()
	underlineSource := info != nil && info.Mode()&os.ModeCharDevice != 0
	for _, t := range list {
		fmt.Println(themeAttribution(t, underlineSource))
	}
	return nil
}

func themeAttribution(t theme.Theme, underlineSource bool) string {
	line := t.Attribution()
	source := t.SourceURL()
	if !underlineSource || source == "" {
		return line
	}
	return strings.TrimSuffix(line, source) + "\x1b[4m" + source + "\x1b[24m"
}

// cmdVis 预览。走的是和真实触发【完全同一条渲染路径】，只是加了 --force
// 跳过时间窗和全屏判断。别为预览另写一套。
func cmdVis(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("要预览哪个主题？gong vis <theme>（gong themes 看有哪些）")
	}
	if len(args) > 1 {
		return fmt.Errorf("参数太多（gong vis <theme>）")
	}
	th, err := theme.Resolve(args[0])
	if err != nil {
		return err
	}
	cmd, err := player.PreviewCommand(th)
	if err != nil {
		return err
	}
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

// cmdFire 是 launchd 入口；位置参数仅为兼容旧版 plist 而忽略。
func cmdFire(_ []string) error {
	c, err := config.Load()
	if err != nil {
		return err
	}

	// 所有定时共用一个 job，launchd 不会告诉我们是谁触发的，按时间反查。
	// 猜错也不至于出事：壳自己的时间窗判断会把不该放的挡掉。
	match := agent.MatchTarget(c, time.Now())
	if match == nil {
		return nil // 一条启用的都没有，安静退出
	}
	s, target := match.Schedule, match.Target
	if !s.Enabled {
		return nil // 停用了就安静退出，不该是错误
	}
	if target.IsZero() {
		return fmt.Errorf("无法计算定时 %s 的目标时刻", s.Ref(match.Index))
	}
	th, err := theme.Select(*s, match.Index, target)
	if err != nil {
		return err
	}
	overlay := paths.Overlay()
	if overlay == "" {
		return fmt.Errorf("找不到 gong-overlay")
	}

	// --tag 只用来给壳的 stderr 打标，不是身份——用序号就够，
	// 不必依赖一个现在已经不强制存在的标签。
	argv := player.ScheduledArgs(overlay, *s, th, target, fmt.Sprintf("#%d", match.Index+1))
	return syscall.Exec(overlay, argv, os.Environ())
}
