package tui

import "github.com/charmbracelet/lipgloss"

// TUI 自己的调色板，刻意不跟任何主题挂钩。
//
// 主题是可增可删的外部资源，TUI 是宿主——从某个具体主题里取色，
// 那个主题被删或改配色之后这里就成了悬空引用（上一版注释就指着一个
// 已经不存在的 --amber 变量）。琥珀是 gong 自己的强调色，仅此而已。
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
