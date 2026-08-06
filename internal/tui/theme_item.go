package tui

import "github.com/xwvike/gong/internal/config"

// themeItem 同时承载真实主题和仅在定时选择器里出现的动态策略。
type themeItem struct{ id string }

func (i themeItem) Title() string { return config.ThemeLabel(i.id) }

// Description 不会在单行主题列表中渲染。
func (i themeItem) Description() string { return "" }

func (i themeItem) FilterValue() string { return i.Title() + " " + i.id }
