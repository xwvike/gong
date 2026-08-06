package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

const (
	themeNameColumnWidth   = 8
	themeAuthorColumnWidth = 12
	themeSourceMaxWidth    = 64
)

// themeItem 同时承载真实主题和仅在定时选择器里出现的动态策略。
type themeItem struct {
	id     string
	author string
	source string
}

func newThemeItem(th theme.Theme) themeItem {
	return themeItem{id: th.ID, author: th.AuthorLabel(), source: th.SourceURL()}
}

func (i themeItem) Title() string {
	name := config.ThemeLabel(i.id)
	if i.author == "" && i.source == "" {
		return name
	}

	title := themeColumn(name, themeNameColumnWidth)
	if i.source == "" {
		return title + i.author
	}
	source := ansi.Truncate(i.source, themeSourceMaxWidth, "...")
	return title + themeColumn(i.author, themeAuthorColumnWidth) + styleSource.Render(source)
}

func themeColumn(value string, width int) string {
	value = ansi.Truncate(value, width, "...")
	return value + strings.Repeat(" ", width-ansi.StringWidth(value))
}

// Description 不会在单行主题列表中渲染。
func (i themeItem) Description() string { return "" }

func (i themeItem) FilterValue() string {
	return strings.Join([]string{config.ThemeLabel(i.id), i.id, i.author, i.source}, " ")
}
