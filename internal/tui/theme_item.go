package tui

import (
	"github.com/xwvike/gong/internal/theme"
)

// themeItem 让 theme.Theme 满足 bubbles/list 的 list.DefaultItem 接口，
// 主题浏览 tab 靠 Title/Description 出效果，FilterValue 决定 "/" 搜的是什么。
type themeItem struct{ t theme.Theme }

func (i themeItem) Title() string { return i.t.ID }

func (i themeItem) Description() string { return "" }

func (i themeItem) FilterValue() string { return i.t.ID }
