// Package theme 解析主题目录。
//
// 主题就是一个目录：<name>/index.html + <name>/theme.toml。
// 用户目录优先、回落到内置——这样 brew upgrade 能更新内置主题，
// 而用户自己写的永远不会被覆盖。
package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/xwvike/gong/internal/paths"
)

type Meta struct {
	Name      string `toml:"name"`      // 展示名，可以是中文
	Desc      string `toml:"desc"`      //
	Lead      int    `toml:"lead"`      // 提前多少秒亮相
	Duration  int    `toml:"duration"`  // 预期可见时长（秒）
	Placement string `toml:"placement"` // center / edge / corner
	WebGL     bool   `toml:"webgl"`
}

type Theme struct {
	ID      string // 目录名，配置里引用的就是它
	Dir     string
	HTML    string
	Builtin bool
	Meta    Meta
}

// 壳那边写死的上限，这里跟着 clamp，免得生成出一条壳会拒绝的命令行
const (
	MaxLead    = 60
	MaxVisible = 60
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
	d += 10
	if d > MaxVisible {
		d = MaxVisible
	}
	return d
}

func (t Theme) Label() string {
	if t.Meta.Name != "" {
		return t.Meta.Name
	}
	return t.ID
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
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return Theme{}, fmt.Errorf("主题名 %q 非法", id)
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
	order := []string{}
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
			t, err := load(filepath.Join(root, e.Name()), builtin)
			if err != nil {
				continue
			}
			if _, dup := byID[t.ID]; !dup {
				order = append(order, t.ID)
			}
			byID[t.ID] = t
		}
	}
	// 先内置后用户：后写的覆盖前面的，正好是「用户优先」
	add(paths.Builtin(), true)
	add(paths.UserThemes(), false)

	sort.Strings(order)
	out := make([]Theme, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}
