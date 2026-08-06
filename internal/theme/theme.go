// Package theme 解析用户优先的主题目录。
package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
)

type Meta struct {
	Lead     int `toml:"lead"`
	Duration int `toml:"duration"`
}

type Theme struct {
	ID      string // 目录名，配置里引用的就是它
	Dir     string
	HTML    string
	Builtin bool
	Meta    Meta
}

// 与渲染壳的硬上限保持一致。
const (
	MaxLead       = 60
	MaxVisible    = 60
	timeoutMargin = 10
)

func (t Theme) LeadSeconds() int {
	if t.Meta.Lead < 0 {
		return 0
	}
	if t.Meta.Lead > MaxLead {
		return MaxLead
	}
	return t.Meta.Lead
}

// TimeoutSeconds 是传给壳的 --timeout：主题自报的时长加一点余量，
// 但绝不超过壳的可见闸门。主题不喊 done 时靠它兜底。
func (t Theme) TimeoutSeconds() int {
	d := t.Meta.Duration
	if d <= 0 {
		d = 10
	}
	if d >= MaxVisible-timeoutMargin {
		d = MaxVisible
	} else {
		d += timeoutMargin
	}
	return d
}

func load(dir string, builtin bool) (Theme, error) {
	t := Theme{ID: filepath.Base(dir), Dir: dir, Builtin: builtin,
		HTML: filepath.Join(dir, "index.html")}
	if _, err := os.Stat(t.HTML); err != nil {
		return t, fmt.Errorf("主题 %s 里没有 index.html", t.ID)
	}
	// theme.toml 缺了不算错，走默认值——主题作者只写 HTML 也该能跑
	metaPath := filepath.Join(dir, "theme.toml")
	if _, err := os.Stat(metaPath); err == nil {
		if _, err := toml.DecodeFile(metaPath, &t.Meta); err != nil {
			return t, fmt.Errorf("主题 %s 的 theme.toml 有问题：%w", t.ID, err)
		}
	}
	return t, nil
}

// Resolve 按「用户目录优先」找主题。
func Resolve(id string) (Theme, error) {
	id = config.NormalizeTheme(id)
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return Theme{}, fmt.Errorf("主题名 %q 非法", id)
	}
	if config.IsThemeStrategy(id) {
		return Theme{}, fmt.Errorf("%q 是定时主题策略，不是可预览的主题", config.ThemeLabel(id))
	}
	for _, root := range []struct {
		dir     string
		builtin bool
	}{
		{paths.UserThemes(), false},
		{paths.Builtin(), true},
	} {
		if root.dir == "" {
			continue
		}
		dir := filepath.Join(root.dir, id)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return load(dir, root.builtin)
		}
	}
	return Theme{}, fmt.Errorf("找不到主题 %q（找过 %s 和 %s）", id, paths.UserThemes(), paths.Builtin())
}

// List 列出所有可用主题，同名时用户的盖住内置的。
func List() []Theme {
	byID := map[string]Theme{}
	add := func(root string, builtin bool) {
		if root == "" {
			return
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// default 是 led 的历史别名，不再占用一个独立主题名。
			if e.Name() == "default" || config.IsThemeStrategy(e.Name()) {
				continue
			}
			if !builtin {
				delete(byID, e.Name())
			}
			t, err := load(filepath.Join(root, e.Name()), builtin)
			if err != nil {
				continue
			}
			byID[t.ID] = t
		}
	}
	// 先内置后用户：后写的覆盖前面的，正好是「用户优先」
	add(paths.Builtin(), true)
	add(paths.UserThemes(), false)

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Theme, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}
