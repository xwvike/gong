// Package config 是 gong 的唯一配置真相。Swift 壳不读任何配置文件，
// 它只接受 Go 算好后内联进来的 flag。
package config

import (
	"fmt"
	"os"
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
	// Label 仅用于展示；定时身份由列表位置决定。
	Label    string `toml:"label"`
	At       string `toml:"at"`       // "HH:MM" 或 "HH:MM:SS"
	Weekdays []int  `toml:"weekdays"` // launchd 口径：0/7=周日，1=周一 … 6=周六
	Theme    string `toml:"theme"`
	Enabled  bool   `toml:"enabled"`
	Grace    int    `toml:"grace"` // 秒。超出这个窗口就不放，防止睡醒后 launchd 补播
}

const (
	CurrentVersion = 1
	DefaultGrace   = 1200
	DefaultTheme   = "led"
	ThemeRandom    = "@random"
	ThemeSequence  = "@sequence"
)

type diskConfig struct {
	Version   int            `toml:"version"`
	Schedules []diskSchedule `toml:"schedule"`
}

type diskSchedule struct {
	Label    string `toml:"label"`
	At       string `toml:"at"`
	Weekdays []int  `toml:"weekdays"`
	Theme    string `toml:"theme"`
	Enabled  bool   `toml:"enabled"`
	Grace    *int   `toml:"grace"`
}

// Default 是 gong on 在没有配置时写下的东西。
// 两条都给了标签只是做个示范——标签不是必须的，序号本身就够用。
func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		Schedules: []Schedule{
			{Label: "午间", At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: ThemeRandom, Enabled: true, Grace: DefaultGrace},
			{Label: "下班", At: "18:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: ThemeRandom, Enabled: true, Grace: DefaultGrace},
		},
	}
}

func Exists() bool {
	_, err := os.Stat(paths.ConfigFile())
	return err == nil
}

func Load() (*Config, error) {
	var disk diskConfig
	if _, err := toml.DecodeFile(paths.ConfigFile(), &disk); err != nil {
		return nil, err
	}
	c := Config{Version: disk.Version, Schedules: make([]Schedule, 0, len(disk.Schedules))}
	for _, raw := range disk.Schedules {
		grace := DefaultGrace
		if raw.Grace != nil {
			grace = *raw.Grace
		}
		c.Schedules = append(c.Schedules, Schedule{
			Label: raw.Label, At: raw.At, Weekdays: raw.Weekdays,
			Theme: NormalizeTheme(raw.Theme), Enabled: raw.Enabled, Grace: grace,
		})
	}
	// 配置文件可以被手动编辑，所以读取时也要校验，避免 gong fire
	// 绕过交互式保存入口直接使用坏配置。
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// NormalizeTheme 把 0.1.x 的内置主题名迁移到新名称。
func NormalizeTheme(id string) string {
	if id == "default" {
		return DefaultTheme
	}
	return id
}

func IsThemeStrategy(id string) bool {
	return id == ThemeRandom || id == ThemeSequence
}

func ThemeLabel(id string) string {
	id = NormalizeTheme(id)
	switch id {
	case ThemeRandom:
		return "随机"
	case ThemeSequence:
		return "顺序"
	default:
		return id
	}
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
	f, err := os.CreateTemp(paths.ConfigDir(), ".config.toml.tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()
	if err := f.Chmod(0o644); err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
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

// Validate 检查配置结构，不检查外部主题资源。
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("配置不能为空")
	}
	if c.Version != CurrentVersion {
		return fmt.Errorf("不支持配置版本 %d，当前只支持版本 %d", c.Version, CurrentVersion)
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
	return os.MkdirAll(paths.UserThemes(), 0o755)
}
