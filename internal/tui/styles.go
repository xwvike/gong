package tui

import "github.com/charmbracelet/lipgloss"

// TUI 使用独立调色板，不依赖可增删的主题资源。
var (
	colAmber = lipgloss.Color("214")
	colDim   = lipgloss.Color("240")
	colGreen = lipgloss.Color("42")
	colRed   = lipgloss.Color("203")
	colFg    = lipgloss.Color("231")
	colBg    = lipgloss.Color("238")
)

var (
	styleTitle = lipgloss.NewStyle().Foreground(colAmber).Bold(true)
	styleDim   = lipgloss.NewStyle().Foreground(colDim)
	styleErr   = lipgloss.NewStyle().Foreground(colRed)
	styleOn    = lipgloss.NewStyle().Foreground(colGreen)
	// 聚焦字段不加 Padding，避免破坏紧凑时间文本的对齐。
	styleField  = lipgloss.NewStyle().Foreground(colFg).Background(colBg)
	styleTabOn  = lipgloss.NewStyle().Foreground(colAmber).Bold(true).Underline(true).Padding(0, 1)
	styleTabOff = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)
	stylePanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colDim).Padding(0, 1)
	styleLabel  = lipgloss.NewStyle().Foreground(colDim).Width(6)

	// bubbles/table 的 renderRow 用 runewidth.Truncate 截断单元格，
	// 那个函数不认 ANSI 转义——颜色只能加在表头/选中行这种整行样式上，
	// 绝不能塞进某个单元格的字符串里，否则截断会把转义序列切烂。
	styleTableHeader   = lipgloss.NewStyle().Bold(true).Foreground(colDim)
	styleTableCell     = lipgloss.NewStyle().Padding(0, 1)
	styleTableSelected = lipgloss.NewStyle().Bold(true).Foreground(colFg).Background(colBg)
)
