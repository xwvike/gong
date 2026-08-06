// Package tui 实现定时管理和主题浏览界面。
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/xwvike/gong/internal/agent"
	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
	"github.com/xwvike/gong/internal/player"
	"github.com/xwvike/gong/internal/theme"
)

type appTab int

const (
	tabSchedules appTab = iota
	tabThemes
)

type mode int

const (
	modeList mode = iota
	modeEdit
	modeLabel
	modeConfirmDelete
)

// 编辑面板里的字段，左右键切主战场、上下键改值。
type field int

const (
	fHour field = iota
	fMinute
	fSecond
	fGrace // 分钟为单位显示，内部仍是秒
	fTheme
	fWeek0 // 之后连续 7 个：一二三四五六日
	fCount = fWeek0 + 7
)

var weekOrder = []int{1, 2, 3, 4, 5, 6, 0}
var weekLabel = []string{"一", "二", "三", "四", "五", "六", "日"}

const (
	graceStepMinutes = 1
	graceMaxMinutes  = 24 * 60 // 24 小时，纯粹是个不让步进器失控的兜底上限
)

type Model struct {
	cfg    *config.Config
	themes []theme.Theme

	tab  appTab
	mode mode
	keys keyMap
	help help.Model

	table table.Model
	list  list.Model
	input textinput.Model

	field        field
	labelReturn  mode // 从 modeList/modeEdit 进 modeLabel 前记一下，改完好回原地
	pickingTheme bool // true：当前在主题库是为了给正在编辑的定时选主题，不是随便逛

	width, height int

	changed   bool
	save      bool
	quitArmed bool
	status    string
	isErr     bool
}

// Run 打开 TUI。返回 true 表示配置被改过、需要保存并接管 launchd。
func Run(c *config.Config) (bool, error) {
	themes := theme.List()
	if len(themes) == 0 {
		return false, fmt.Errorf("一个主题都没找到（找过 %s 和 %s）", paths.UserThemes(), paths.Builtin())
	}
	m := newModel(c, themes)
	p := tea.NewProgram(m, tea.WithAltScreen())
	out, err := p.Run()
	if err != nil {
		return false, err
	}
	fin := out.(Model)
	return fin.save && fin.changed, nil
}

func newModel(c *config.Config, themes []theme.Theme) Model {
	m := Model{
		cfg:    c,
		themes: themes,
		keys:   newKeyMap(),
		help:   help.New(),
		table:  newScheduleTable(),
		list:   newThemeList(themes),
		input:  newLabelInput(),
	}
	m.refreshTable()
	return m
}

func newScheduleTable() table.Model {
	cols := []table.Column{
		{Title: "", Width: 1},
		{Title: "#", Width: 3},
		{Title: "标签", Width: 8}, // 可选，没设就显示 "—"，不是必填项
		{Title: "时间", Width: 8},
		{Title: "星期", Width: 10},
		{Title: "主题", Width: 14},
		{Title: "唤醒", Width: 5},
	}
	// 空格留给启停、"d" 留给删除——都是这个 app 自己的按键，
	// table 默认键位里 PageDown 含空格、HalfPageDown 含 "d"，先挪开。
	tk := table.DefaultKeyMap()
	tk.PageDown = key.NewBinding(key.WithKeys("f", "pgdown"), key.WithHelp("f/pgdn", "翻页"))
	tk.HalfPageUp = key.NewBinding()
	tk.HalfPageDown = key.NewBinding()

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithKeyMap(tk),
	)
	t.SetStyles(table.Styles{
		Header:   styleTableHeader,
		Cell:     styleTableCell,
		Selected: styleTableSelected,
	})
	return t
}

func newThemeList(themes []theme.Theme) list.Model {
	items := make([]list.Item, len(themes))
	for i, t := range themes {
		items[i] = themeItem{t}
	}
	// 主题仅展示名称，每项占一行。
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false // 关掉后 Height() 恒为 1，不用再 SetHeight
	delegate.SetSpacing(0)           // 默认条间留 1 行，三个名字会散成一屏
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(colAmber).BorderForeground(colAmber)

	l := list.New(items, delegate, 0, 0)
	// 标题和帮助由外层统一渲染。
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)       // 帮助统一走底部 help.Model，不要两份footer
	l.SetShowPagination(false) // 装不下时 applyLayout 会再打开
	// 退出由根模型处理，确保未保存改动会先确认。
	l.KeyMap.Quit = key.NewBinding()
	l.KeyMap.ForceQuit = key.NewBinding()
	return l
}

// newLabelInput 接受可为空的展示标签。
func newLabelInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "标签 › "
	ti.CharLimit = 40
	return ti
}

func (m Model) Init() tea.Cmd { return nil }

// ---- 数据 <-> 组件同步 ----

func (m *Model) selected() *config.Schedule {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.cfg.Schedules) {
		return nil
	}
	return &m.cfg.Schedules[i]
}

// refreshTable 把 cfg.Schedules 重新铺进表格，尽量保住光标位置。
// 结构性改动（增删定时、改标签）之后都要调一次，否则表格和配置会对不上。
func (m *Model) refreshTable() {
	rows := make([]table.Row, 0, len(m.cfg.Schedules))
	for i, s := range m.cfg.Schedules {
		dot := "○"
		if s.Enabled {
			dot = "●"
		}
		wake := "—"
		if s.Enabled {
			tr := agent.TriggerFor(s)
			wake = fmt.Sprintf("%02d:%02d", tr.Hour, tr.Minute)
		}
		label := s.Label
		if label == "" {
			label = "—" // 标签可选，没有就留个占位符，别显示成空白像渲染坏了
		}
		// 主题资源错误由表格外的状态行显示。
		rows = append(rows, table.Row{dot, fmt.Sprintf("#%d", i+1), label, s.At, s.WeekdaysLabel(), s.Theme, wake})
	}
	cursor := m.table.Cursor()
	m.table.SetRows(rows)
	switch {
	case len(rows) == 0:
	case cursor >= len(rows):
		m.table.SetCursor(len(rows) - 1)
	default:
		m.table.SetCursor(cursor)
	}
	// 行数变了（增/删定时）表格该占的高度也变了，不等下一次终端 resize 就重算。
	m.applyLayout()
}

func (m *Model) themeIndexInList(id string) int {
	for i, item := range m.list.Items() {
		if item.(themeItem).t.ID == id {
			return i
		}
	}
	return 0
}

// ---- 尺寸 ----

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.applyLayout()
}

// applyLayout 按内容量和终端尺寸约束 table/list/help 高度。
func (m *Model) applyLayout() {
	w, h := m.width, m.height
	m.help.Width = w

	const chrome = 6 // 标题/tab 一行、空行、告警预留、状态一行、空行、帮助一行
	editH := 0
	if m.tab == tabSchedules && m.mode == modeEdit {
		editH = 9
	}
	contentH := max(3, h-chrome-editH)

	// table 高度包含表头，因此数据行数需要额外加一。
	wantRows := max(1, len(m.cfg.Schedules))
	m.table.SetWidth(w)
	m.table.SetHeight(min(contentH, wantRows+1))

	// list 始终为过滤输入框保留一行。
	const listChrome = 1
	itemLines := max(1, len(m.themes)) // delegate 压成了 1 行/条、条间无间距

	// 仅在内容溢出时显示分页提示。
	m.list.SetShowPagination(itemLines+listChrome > contentH)
	m.list.SetSize(w, min(contentH, itemLines+listChrome))
}

// ---- Update ----

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeLabel:
		m.quitArmed = false
		return m.updateLabel(msg)
	case modeConfirmDelete:
		m.quitArmed = false
		return m.updateConfirm(msg)
	}

	// 主题库过滤输入中：所有按键原样转发给 list，
	// 包括 esc/enter，它们此刻是「取消过滤」「应用过滤」，不是本 app 的含义。
	if m.tab == tabThemes && m.list.SettingFilter() {
		m.quitArmed = false
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	// 全局键：退出、保存、帮助——任何浏览态下都认，编辑/改标签/确认删除已在上面拦掉。
	if key.Matches(msg, m.keys.Quit) {
		return m.handleQuit()
	}
	m.quitArmed = false
	switch {
	case key.Matches(msg, m.keys.Save):
		return m.handleSave()
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.TabNext):
		// 编辑中或正在为某条定时选主题时不许切 tab，先用 esc/enter 把这件事了结，
		// 不然容易半途切走、定时停在一半改不完的状态里。
		if m.mode == modeEdit || m.pickingTheme {
			return m, nil
		}
		if m.tab == tabSchedules {
			m.tab = tabThemes
		} else {
			m.tab = tabSchedules
		}
		m.status = ""
		m.resize(m.width, m.height)
		return m, nil
	}

	if m.tab == tabThemes {
		return m.updateThemes(msg)
	}
	if m.mode == modeEdit {
		return m.updateEdit(msg)
	}
	return m.updateList(msg)
}

func (m Model) handleQuit() (tea.Model, tea.Cmd) {
	if m.changed && !m.quitArmed {
		m.status = "有未保存的改动，按 s 保存退出，再按一次 q 放弃"
		m.isErr = false
		m.quitArmed = true
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) handleSave() (tea.Model, tea.Cmd) {
	if err := m.cfg.Validate(); err != nil {
		m.status = "存不了：" + err.Error()
		m.isErr = true
		return m, nil
	}
	available := make(map[string]bool, len(m.themes))
	for _, th := range m.themes {
		available[th.ID] = true
	}
	for i, schedule := range m.cfg.Schedules {
		if schedule.Enabled && !available[schedule.Theme] {
			m.status = fmt.Sprintf("存不了：%s 的主题 %q 找不到", schedule.Ref(i), schedule.Theme)
			m.isErr = true
			return m, nil
		}
	}
	if err := agent.ValidateTriggerSlots(m.cfg); err != nil {
		m.status = "存不了：" + err.Error()
		m.isErr = true
		return m, nil
	}
	m.save = true
	return m, tea.Quit
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Toggle):
		if s := m.selected(); s != nil {
			s.Enabled = !s.Enabled
			m.changed = true
			m.status = ""
			m.refreshTable()
		}
		return m, nil

	case key.Matches(msg, m.keys.Edit):
		if m.selected() != nil {
			m.mode = modeEdit
			m.field = fHour
			m.status = ""
			m.resize(m.width, m.height)
		}
		return m, nil

	case key.Matches(msg, m.keys.Add):
		// 不用取标签、不用管重不重复——新的一条排在最后，序号自己就是 #N。
		m.cfg.Schedules = append(m.cfg.Schedules, config.Schedule{
			At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5},
			Theme: m.defaultThemeID(), Enabled: true, Grace: config.DefaultGrace,
		})
		m.changed = true
		m.refreshTable()
		m.table.SetCursor(len(m.cfg.Schedules) - 1)
		m.mode = modeEdit
		m.field = fHour
		m.status = fmt.Sprintf("新建了 #%d，想要标签的话按 r（可选）", len(m.cfg.Schedules))
		m.isErr = false
		m.resize(m.width, m.height)
		return m, nil

	case key.Matches(msg, m.keys.Delete):
		if m.selected() != nil {
			m.mode = modeConfirmDelete
		}
		return m, nil

	case key.Matches(msg, m.keys.EditLabel):
		if s := m.selected(); s != nil {
			m.labelReturn = modeList
			m.mode = modeLabel
			m.input.SetValue(s.Label)
			m.input.CursorEnd()
			return m, m.input.Focus()
		}
		return m, nil

	case key.Matches(msg, m.keys.Preview):
		m.status, m.isErr = m.previewSchedule()
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.selected()
	if s == nil {
		m.mode = modeList
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Edit):
		m.mode = modeList
		m.resize(m.width, m.height)
		return m, nil
	case key.Matches(msg, m.keys.EditLabel):
		m.labelReturn = modeEdit
		m.mode = modeLabel
		m.input.SetValue(s.Label)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case key.Matches(msg, m.keys.Preview):
		m.status, m.isErr = m.previewSchedule()
		return m, nil
	case key.Matches(msg, m.keys.PickTheme):
		m.pickingTheme = true
		m.tab = tabThemes
		m.list.ResetFilter()
		m.list.Select(m.themeIndexInList(s.Theme))
		m.resize(m.width, m.height)
		return m, nil
	case key.Matches(msg, m.keys.FieldLeft):
		if m.field > 0 {
			m.field--
		}
		return m, nil
	case key.Matches(msg, m.keys.FieldRight):
		if m.field < fCount-1 {
			m.field++
		}
		return m, nil
	case key.Matches(msg, m.keys.ValueUp):
		m.bump(s, +1)
		return m, nil
	case key.Matches(msg, m.keys.ValueDown):
		m.bump(s, -1)
		return m, nil
	}
	return m, nil
}

// bump 把当前字段加减一格。时间字段回绕，宽限限幅，主题在列表里循环。
func (m *Model) bump(s *config.Schedule, dir int) {
	switch {
	case m.field == fHour:
		s.At = config.FormatClock(wrap(s.Seconds()+dir*3600, 86400))
	case m.field == fMinute:
		s.At = config.FormatClock(wrap(s.Seconds()+dir*60, 86400))
	case m.field == fSecond:
		s.At = config.FormatClock(wrap(s.Seconds()+dir, 86400))
	case m.field == fGrace:
		minutes := min(max(s.Grace/60+dir*graceStepMinutes, 0), graceMaxMinutes)
		s.Grace = minutes * 60
	case m.field == fTheme:
		i := wrap(m.themeIndexOf(s.Theme)+dir, len(m.themes))
		s.Theme = m.themes[i].ID
	case m.field >= fWeek0:
		day := weekOrder[m.field-fWeek0]
		set := map[int]bool{}
		for _, d := range s.Weekdays {
			set[d%7] = true
		}
		if set[day] {
			delete(set, day)
		} else {
			set[day] = true
		}
		days := make([]int, 0, len(set))
		for d := range set {
			days = append(days, d)
		}
		sort.Ints(days)
		s.Weekdays = days
	default:
		return
	}
	m.changed = true
	m.refreshTable()
}

// defaultThemeID 优先选择默认主题，缺失时回落到首个可用主题。
func (m *Model) defaultThemeID() string {
	for _, t := range m.themes {
		if t.ID == config.DefaultTheme {
			return t.ID
		}
	}
	return m.themes[0].ID
}

func (m *Model) themeIndexOf(id string) int {
	for i, t := range m.themes {
		if t.ID == id {
			return i
		}
	}
	return 0
}

func wrap(v, n int) int { return ((v % n) + n) % n }

// updateLabel 接受空值和重复值。
func (m Model) updateLabel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if s := m.selected(); s != nil {
			label := strings.TrimSpace(m.input.Value())
			if s.Label != label {
				s.Label = label
				m.changed = true
			}
		}
		m.input.Blur()
		m.mode = m.labelReturn
		m.refreshTable()
		return m, nil
	case tea.KeyEsc:
		m.input.Blur()
		m.mode = m.labelReturn
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "y" || msg.String() == "Y" {
		i := m.table.Cursor()
		if s := m.selected(); s != nil {
			m.status = "已删除 " + s.Ref(i) + "（保存后生效）"
			m.isErr = false
			m.cfg.Schedules = append(m.cfg.Schedules[:i], m.cfg.Schedules[i+1:]...)
			m.changed = true
			m.refreshTable()
		}
	}
	m.mode = modeList
	return m, nil
}

func (m Model) updateThemes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Preview):
		if it, ok := m.list.SelectedItem().(themeItem); ok {
			m.status, m.isErr = m.previewTheme(it.t)
		}
		return m, nil
	case key.Matches(msg, m.keys.Confirm):
		if !m.pickingTheme {
			return m, nil // 单纯逛主题库时 enter 不做事，预览走 v
		}
		if it, ok := m.list.SelectedItem().(themeItem); ok {
			if s := m.selected(); s != nil {
				s.Theme = it.t.ID
				m.changed = true
				m.refreshTable()
			}
		}
		m.pickingTheme = false
		m.tab = tabSchedules
		m.mode = modeEdit
		m.resize(m.width, m.height)
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		if m.pickingTheme {
			m.pickingTheme = false
			m.tab = tabSchedules
			m.mode = modeEdit
			m.resize(m.width, m.height)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// previewSchedule/previewTheme 走的是和真实触发完全同一条渲染路径，只是多了
// --force。不阻塞 TUI：浮层是 GUI 窗口，不占用终端。
func (m Model) previewSchedule() (string, bool) {
	s := m.selected()
	if s == nil {
		return "", false
	}
	th, err := theme.Resolve(s.Theme)
	if err != nil {
		return err.Error(), true
	}
	return m.previewTheme(th)
}

func (m Model) previewTheme(th theme.Theme) (string, bool) {
	cmd, err := player.PreviewCommand(th)
	if err != nil {
		return err.Error(), true
	}
	if err := cmd.Start(); err != nil {
		return err.Error(), true
	}
	go cmd.Wait() // 收尸，别留僵尸进程
	return "正在预览 " + th.ID, false
}
