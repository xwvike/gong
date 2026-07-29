// Package config 是 gong 的唯一配置真相。Swift 壳不读任何配置文件，
// 它只接受 Go 算好后内联进来的 flag。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/xwvike/gong/internal/paths"
)

type Config struct {
	Version   int        `toml:"version"`
	Schedules []Schedule `toml:"schedule"`
}

type Schedule struct {
	// Label 纯粹给人看，完全可选，留空就留空。不参与任何匹配、不拼进
	// launchd label 或文件名，所以也没有字符限制——那些顾虑都是「名字曾经
	// 是标识符」那版设计留下的，现在标识符是它在列表里的位置（序号）。
	Label    string `toml:"label"`
	At       string `toml:"at"`       // "HH:MM" 或 "HH:MM:SS"
	Weekdays []int  `toml:"weekdays"` // launchd 口径：0/7=周日，1=周一 … 6=周六
	Theme    string `toml:"theme"`
	Enabled  bool   `toml:"enabled"`
	Grace    int    `toml:"grace"` // 秒。超出这个窗口就不放，防止睡醒后 launchd 补播
}

const DefaultGrace = 1200

// DefaultTheme 是 Go 唯一硬依赖的主题名。
//
// 主题体系本身是纯目录扫描的，增删主题不需要改任何 Go/Swift 代码——
// 唯独「没得选的时候选谁」需要一个字面量兜底。把它收在这一个常量里，
// 就是为了让这份依赖是显式的、可数的：全项目只有这里知道 "default"
// 这个名字，别处一律走 theme.List() / theme.Resolve()。
//
// 注意它不保证存在：内置目录被删或没装好时 Resolve 会失败，
// 调用方按「主题找不到」降级处理，不要假设这个名字一定解析得开。
const DefaultTheme = "default"

// Default 是 gong on 在没有配置时写下的东西。
// 两条都给了标签只是做个示范——标签不是必须的，序号本身就够用。
func Default() *Config {
	return &Config{
		Version: 1,
		Schedules: []Schedule{
			{Label: "午间", At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: DefaultTheme, Enabled: true, Grace: DefaultGrace},
			{Label: "下班", At: "18:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: DefaultTheme, Enabled: true, Grace: DefaultGrace},
		},
	}
}

func Exists() bool {
	_, err := os.Stat(paths.ConfigFile())
	return err == nil
}

func Load() (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(paths.ConfigFile(), &c); err != nil {
		return nil, err
	}
	for i := range c.Schedules {
		if c.Schedules[i].Grace == 0 {
			c.Schedules[i].Grace = DefaultGrace
		}
	}
	// 配置文件可以被手动编辑，所以读取时也要校验，避免 gong fire
	// 绕过交互式保存入口直接使用坏配置。
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// LoadOrDefault 让 ls / set 在还没 gong on 过的时候也能跑。
func LoadOrDefault() (*Config, error) {
	if !Exists() {
		return Default(), nil
	}
	return Load()
}

func (c *Config) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.ConfigDir(), 0o755); err != nil {
		return err
	}
	// 先写临时文件再 rename：写到一半断电不会留下半个配置
	tmp := paths.ConfigFile() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, paths.ConfigFile())
}

// At 按位置取一条定时（0-based）。定时的身份就是它在列表里的位置，
// 命令行和 TUI 里显示的「#N」都是 N = i+1。
func (c *Config) At(i int) (*Schedule, bool) {
	if i < 0 || i >= len(c.Schedules) {
		return nil, false
	}
	return &c.Schedules[i], true
}

// RemoveAt 按位置删一条，i 是 0-based 下标。
func (c *Config) RemoveAt(i int) bool {
	if i < 0 || i >= len(c.Schedules) {
		return false
	}
	c.Schedules = append(c.Schedules[:i], c.Schedules[i+1:]...)
	return true
}

// Ref 是消息里指代一条定时的统一写法：有标签用标签，没有就用序号。
func (s Schedule) Ref(index int) string {
	if s.Label != "" {
		return s.Label
	}
	return fmt.Sprintf("#%d", index+1)
}

// ---- 时间 ----

// ParseClock 把 "18:00" / "18:00:00" 解析成当天的秒数。
func ParseClock(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("时间格式应该是 HH:MM 或 HH:MM:SS，收到 %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("时间里有非数字：%q", s)
		}
		nums[i] = n
	}
	h, m, sec := nums[0], nums[1], nums[2]
	if h < 0 || h > 23 || m < 0 || m > 59 || sec < 0 || sec > 59 {
		return 0, fmt.Errorf("时间越界：%q", s)
	}
	return h*3600 + m*60 + sec, nil
}

func FormatClock(secs int) string {
	secs = ((secs % 86400) + 86400) % 86400
	return fmt.Sprintf("%02d:%02d:%02d", secs/3600, (secs/60)%60, secs%60)
}

func (s Schedule) Seconds() int {
	v, err := ParseClock(s.At)
	if err != nil {
		return 0
	}
	return v
}

var weekdayNames = map[int]string{0: "日", 1: "一", 2: "二", 3: "三", 4: "四", 5: "五", 6: "六", 7: "日"}

func (s Schedule) WeekdaysLabel() string {
	if len(s.Weekdays) == 0 {
		return "—"
	}
	set := map[int]bool{}
	for _, d := range s.Weekdays {
		set[d%7] = true
	}
	if len(set) == 7 {
		return "每天"
	}
	if len(set) == 5 && !set[0] && !set[6] {
		return "工作日"
	}
	days := make([]int, 0, len(set))
	for d := range set {
		days = append(days, d)
	}
	sort.Ints(days)
	var b strings.Builder
	for _, d := range days {
		b.WriteString(weekdayNames[d])
	}
	return b.String()
}

// Validate 在保存前挡住会生成坏 plist 的配置。
//
// 注意这里不再检查「名字」——标签是可选的自由文本，没有唯一性要求，
// 也没有字符限制，因为它不再拼进 launchd label 或文件名。出错信息里
// 用 Ref(i) 指代第 i 条，标签留空时自动落回 "#序号"。
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("配置不能为空")
	}
	for i, s := range c.Schedules {
		ref := s.Ref(i)
		if _, err := ParseClock(s.At); err != nil {
			return fmt.Errorf("定时 %s：%w", ref, err)
		}
		if len(s.Weekdays) == 0 {
			return fmt.Errorf("定时 %s 一个星期几都没选", ref)
		}
		for _, d := range s.Weekdays {
			if d < 0 || d > 7 {
				return fmt.Errorf("定时 %s：星期几必须是 0 到 7，收到 %d", ref, d)
			}
		}
		if s.Theme == "" {
			return fmt.Errorf("定时 %s 没有主题", ref)
		}
		if s.Theme == "." || s.Theme == ".." || strings.ContainsAny(s.Theme, `/\\`) {
			return fmt.Errorf("定时 %s：主题名不能含路径分隔符", ref)
		}
		if s.Grace < 0 {
			return fmt.Errorf("定时 %s：grace 不能是负数", ref)
		}
	}
	return nil
}

// EnsureUserThemeDir 建好用户主题目录，让人知道往哪放自己的主题。
func EnsureUserThemeDir() error {
	return os.MkdirAll(filepath.Join(paths.UserThemes()), 0o755)
}
