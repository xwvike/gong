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
	Name     string `toml:"name"`
	At       string `toml:"at"`       // "HH:MM" 或 "HH:MM:SS"
	Weekdays []int  `toml:"weekdays"` // launchd 口径：0/7=周日，1=周一 … 6=周六
	Theme    string `toml:"theme"`
	Enabled  bool   `toml:"enabled"`
	Grace    int    `toml:"grace"` // 秒。超出这个窗口就不放，防止睡醒后 launchd 补播
}

const DefaultGrace = 1200

// Default 是 gong on 在没有配置时写下的东西。
func Default() *Config {
	return &Config{
		Version: 1,
		Schedules: []Schedule{
			{Name: "noon", At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: "nixie", Enabled: true, Grace: DefaultGrace},
			{Name: "evening", At: "18:00:00", Weekdays: []int{1, 2, 3, 4, 5},
				Theme: "default", Enabled: true, Grace: DefaultGrace},
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

func (c *Config) Find(name string) (*Schedule, int) {
	for i := range c.Schedules {
		if c.Schedules[i].Name == name {
			return &c.Schedules[i], i
		}
	}
	return nil, -1
}

func (c *Config) Remove(name string) bool {
	_, i := c.Find(name)
	if i < 0 {
		return false
	}
	c.Schedules = append(c.Schedules[:i], c.Schedules[i+1:]...)
	return true
}

// UniqueName 给新建的定时找一个没被占用的名字。
func (c *Config) UniqueName(base string) string {
	if _, i := c.Find(base); i < 0 {
		return base
	}
	for n := 2; ; n++ {
		cand := base + strconv.Itoa(n)
		if _, i := c.Find(cand); i < 0 {
			return cand
		}
	}
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
func (c *Config) Validate() error {
	seen := map[string]bool{}
	for _, s := range c.Schedules {
		if s.Name == "" {
			return fmt.Errorf("有一条定时没有名字")
		}
		if strings.ContainsAny(s.Name, " /.\\\t") {
			return fmt.Errorf("定时名 %q 不能含空格、点、斜杠——它要拼进 launchd label 和文件名", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("定时名重复：%s", s.Name)
		}
		seen[s.Name] = true
		if _, err := ParseClock(s.At); err != nil {
			return fmt.Errorf("定时 %s：%w", s.Name, err)
		}
		if len(s.Weekdays) == 0 {
			return fmt.Errorf("定时 %s 一个星期几都没选", s.Name)
		}
		if s.Theme == "" {
			return fmt.Errorf("定时 %s 没有主题", s.Name)
		}
	}
	return nil
}

// EnsureUserThemeDir 建好用户主题目录，让人知道往哪放自己的主题。
func EnsureUserThemeDir() error {
	return os.MkdirAll(filepath.Join(paths.UserThemes()), 0o755)
}
