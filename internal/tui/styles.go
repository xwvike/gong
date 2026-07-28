package tui

import "github.com/charmbracelet/lipgloss"

// 调色板跟主题里那个琥珀色一脉相承（themes/default 的 --amber #F2C14E），
// 整个项目就这一处强调色，TUI 没理由另起一套。
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
	// styleField 标出编辑面板里当前聚焦的那个字段。
	// 故意不加 Padding：这些字段大多是冒号连着的紧凑文本（12:00:00、一二三四五），
	// 加左右留白会把邻居的字符顶开一格，破坏对齐——之前就因为这个把 "12:00:00"
	// 挤成过 "12: 00 :00"，靠背景色区分聚焦态就够了。
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
