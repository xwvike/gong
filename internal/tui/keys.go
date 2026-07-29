package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap 是全局按键，help.Model 靠它自动生成底部提示、按 ? 展开成全帮助。
// 每个模式再各自定义一份「这一刻实际能按哪些」，见 tui.go 里的 contextHelp。
type keyMap struct {
	Quit    key.Binding
	Save    key.Binding
	Help    key.Binding
	TabNext key.Binding

	Toggle key.Binding
	Add    key.Binding
	Delete key.Binding
	Edit   key.Binding
	// EditLabel 编辑的是那条可选的装饰性标签，不是身份——身份永远是序号。
	EditLabel key.Binding
	Preview   key.Binding

	FieldLeft  key.Binding
	FieldRight key.Binding
	ValueUp    key.Binding
	ValueDown  key.Binding
	PickTheme  key.Binding
	Back       key.Binding

	Confirm key.Binding
	Cancel  key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
		Save:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "保存并接管 launchd")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "展开帮助")),
		TabNext: key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "切换 定时/主题")),

		Toggle:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "启停")),
		Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "新增")),
		Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "删除")),
		Edit:      key.NewBinding(key.WithKeys("e", "enter"), key.WithHelp("enter", "编辑")),
		EditLabel: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "设标签")),
		Preview:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "预览")),

		FieldLeft:  key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/→", "选字段")),
		FieldRight: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("", "")),
		ValueUp:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "改值")),
		ValueDown:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("", "")),
		PickTheme:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "打开主题库选")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "回列表")),

		Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "确认")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "取消")),
	}
}

// contextHelp 满足 help.KeyMap 接口；每次 View() 按当前模式现造一份，
// 而不是让 keyMap 自己承担「此刻该显示什么」的判断。
type contextHelp struct {
	short []key.Binding
	full  [][]key.Binding
}

func (h contextHelp) ShortHelp() []key.Binding  { return h.short }
func (h contextHelp) FullHelp() [][]key.Binding { return h.full }
func short(bs ...key.Binding) contextHelp       { return contextHelp{short: bs, full: [][]key.Binding{bs}} }
func full(groups ...[]key.Binding) contextHelp {
	var s []key.Binding
	for _, g := range groups {
		s = append(s, g...)
	}
	return contextHelp{short: s, full: groups}
}
