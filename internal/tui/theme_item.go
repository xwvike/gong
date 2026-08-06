package tui

import (
	"github.com/xwvike/gong/internal/theme"
)

// themeItem 让 theme.Theme 满足 bubbles/list 的 list.DefaultItem 接口。
type themeItem struct{ t theme.Theme }

func (i themeItem) Title() string { return i.t.ID }

// Description 不会在单行主题列表中渲染。
func (i themeItem) Description() string { return "" }

func (i themeItem) FilterValue() string { return i.t.ID }
