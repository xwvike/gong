package tui

import (
	"fmt"

	"github.com/xwvike/gong/internal/theme"
)

// themeItem 让 theme.Theme 满足 bubbles/list 的 list.DefaultItem 接口，
// 主题浏览 tab 靠 Title/Description 出效果，FilterValue 决定 "/" 搜的是什么。
type themeItem struct{ t theme.Theme }

func (i themeItem) Title() string { return i.t.Label() }

func (i themeItem) Description() string {
	extra := fmt.Sprintf("提前 %ds 亮相 · %s", i.t.LeadSeconds(), placementLabel(i.t.Meta.Placement))
	if i.t.Meta.Desc == "" {
		return extra
	}
	return i.t.Meta.Desc + "  " + styleDim.Render(extra)
}

func (i themeItem) FilterValue() string { return i.t.Label() + " " + i.t.ID }

func placementLabel(p string) string {
	switch p {
	case "center":
		return "挡屏幕中央"
	case "edge":
		return "贴边不挡视线"
	case "corner":
		return "角落"
	default:
		return "位置未知"
	}
}
