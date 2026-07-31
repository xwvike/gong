package tui

import (
	"github.com/xwvike/gong/internal/theme"
)

// themeItem 让 theme.Theme 满足 bubbles/list 的 list.DefaultItem 接口。
// 列表只展示主题名，想知道长什么样就 v 预览——描述文案已经全部删掉了。
type themeItem struct{ t theme.Theme }

func (i themeItem) Title() string { return i.t.ID }

// 接口要求有这个方法，但 delegate 关了 ShowDescription，返回值不会被渲染。
func (i themeItem) Description() string { return "" }

func (i themeItem) FilterValue() string { return i.t.ID }
