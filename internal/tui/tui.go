// Package tui 是 gong set：一屏之内把定时增删改查完。
//
// 刻意不做文本输入框：时间和星期都用方向键调，省掉一整套校验和错误态。
// 只有重命名需要打字，那一个手写了个最小的输入。
package tui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
	"github.com/xwvike/gong/internal/theme"
)

var (
	cDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cAmber  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cOn     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cOff    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	cField  = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("238"))
	cTitle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	cKeyCap = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

type mode int

const (
	modeList mode = iota
	modeEdit
	modeRename
	modeConfirmDelete
)

// 星期在界面上按「一二三四五六日」排，值用 launchd 口径（1=周一，0=周日）
var weekOrder = []int{1, 2, 3, 4, 5, 6, 0}
var weekLabel = []string{"一", "二", "三", "四", "五", "六", "日"}

// 编辑态里的字段：时 分 秒 主题 然后七个星期
const (
	fHour = iota
	fMinute
	fSecond
	fTheme
	fWeek0 // 之后连续 7 个
	fCount = fWeek0 + 7
)

type model struct {
	cfg     *config.Config
	themes  []theme.Theme
	cursor  int
	field   int
	mode    mode
	changed bool
	status  string
	buf     string // 重命名时的输入缓冲
	quit    bool
	save    bool
}

// Run 打开 TUI。返回 true 表示配置被改过、需要保存。
func Run(c *config.Config) (bool, error) {
	m := model{cfg: c, themes: theme.List()}
	if len(m.themes) == 0 {
		return false, fmt.Errorf("一个主题都没找到（找过 %s 和 %s）", paths.UserThemes(), paths.Builtin())
	}
	p := tea.NewProgram(m)
	out, err := p.Run()
	if err != nil {
		return false, err
	}
	fin := out.(model)
	return fin.save && fin.changed, nil
}

func (m model) Init() tea.Cmd { return nil }

func (m *model) cur() *config.Schedule {
	if m.cursor < 0 || m.cursor >= len(m.cfg.Schedules) {
		return nil
	}
	return &m.cfg.Schedules[m.cursor]
}

func (m *model) themeIndex(id string) int {
	for i, t := range m.themes {
		if t.ID == id {
			return i
		}
	}
	return 0
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.mode {
	case modeRename:
		return m.updateRename(key)
	case modeConfirmDelete:
		return m.updateConfirm(key)
	case modeEdit:
		return m.updateEdit(key)
	default:
		return m.updateList(key)
	}
}

func (m model) updateList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc", "ctrl+c":
		if m.changed {
			m.status = "有未保存的改动，按 s 保存退出，再按一次 q 放弃"
			m.changed = false // 第二次 q 就真走了
			return m, nil
		}
		m.quit = true
		return m, tea.Quit

	case "s":
		if err := m.cfg.Validate(); err != nil {
			m.status = cErr.Render("存不了：" + err.Error())
			return m, nil
		}
		m.save = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.cfg.Schedules)-1 {
			m.cursor++
		}

	case " ":
		if s := m.cur(); s != nil {
			s.Enabled = !s.Enabled
			m.changed = true
			m.status = ""
		}

	case "e", "enter":
		if m.cur() != nil {
			m.mode = modeEdit
			m.field = fHour
			m.status = ""
		}

	case "a":
		name := m.cfg.UniqueName("timer")
		m.cfg.Schedules = append(m.cfg.Schedules, config.Schedule{
			Name: name, At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5},
			Theme: m.themes[0].ID, Enabled: true, Grace: config.DefaultGrace,
		})
		m.cursor = len(m.cfg.Schedules) - 1
		m.changed = true
		m.mode = modeEdit
		m.field = fHour
		m.status = "新建了 " + name + "，按 r 改名"

	case "d":
		if m.cur() != nil {
			m.mode = modeConfirmDelete
		}

	case "r":
		if s := m.cur(); s != nil {
			m.mode = modeRename
			m.buf = s.Name
		}

	case "v":
		m.status = m.preview()
	}
	return m, nil
}

func (m model) updateEdit(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.cur()
	if s == nil {
		m.mode = modeList
		return m, nil
	}
	switch key.String() {
	case "esc", "enter", "q":
		m.mode = modeList
		return m, nil
	case "r":
		m.mode = modeRename
		m.buf = s.Name
		return m, nil
	case "v":
		m.status = m.preview()
		return m, nil
	case "left", "h":
		if m.field > 0 {
			m.field--
		}
	case "right", "l":
		if m.field < fCount-1 {
			m.field++
		}
	case "up", "k":
		m.bump(s, +1)
	case "down", "j":
		m.bump(s, -1)
	case " ":
		if m.field >= fWeek0 {
			m.bump(s, +1)
		}
	}
	return m, nil
}

// bump 把当前字段加减一格。时间字段回绕，主题在列表里循环。
func (m *model) bump(s *config.Schedule, dir int) {
	secs := s.Seconds()
	switch {
	case m.field == fHour:
		secs = wrap(secs+dir*3600, 86400)
	case m.field == fMinute:
		secs = wrap(secs+dir*60, 86400)
	case m.field == fSecond:
		secs = wrap(secs+dir, 86400)
	case m.field == fTheme:
		i := m.themeIndex(s.Theme)
		i = wrap(i+dir, len(m.themes))
		s.Theme = m.themes[i].ID
		m.changed = true
		return
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
		m.changed = true
		return
	default:
		return
	}
	s.At = config.FormatClock(secs)
	m.changed = true
}

func wrap(v, n int) int { return ((v % n) + n) % n }

func (m model) updateRename(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		s := m.cur()
		name := strings.TrimSpace(m.buf)
		if name == "" {
			m.status = cErr.Render("名字不能为空")
			m.mode = modeList
			return m, nil
		}
		if other, i := m.cfg.Find(name); other != nil && i != m.cursor {
			m.status = cErr.Render("已经有叫 " + name + " 的定时了")
			return m, nil
		}
		if strings.ContainsAny(name, " /.\\\t") {
			m.status = cErr.Render("名字不能含空格、点、斜杠——它要拼进 launchd label")
			return m, nil
		}
		if s.Name != name {
			s.Name = name
			m.changed = true
		}
		m.mode = modeList
	case tea.KeyEsc:
		m.mode = modeList
	case tea.KeyBackspace:
		if r := []rune(m.buf); len(r) > 0 {
			m.buf = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.buf += string(key.Runes)
	}
	return m, nil
}

func (m model) updateConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "y", "Y":
		if s := m.cur(); s != nil {
			m.status = "已删除 " + s.Name + "（保存后生效）"
			m.cfg.Schedules = append(m.cfg.Schedules[:m.cursor], m.cfg.Schedules[m.cursor+1:]...)
			if m.cursor >= len(m.cfg.Schedules) {
				m.cursor = len(m.cfg.Schedules) - 1
			}
			m.changed = true
		}
	}
	m.mode = modeList
	return m, nil
}

// preview 走的是和真实触发完全同一条渲染路径，只是多了 --force。
// 不阻塞 TUI：浮层是 GUI，不碰终端。
func (m model) preview() string {
	s := m.cur()
	if s == nil {
		return ""
	}
	th, err := theme.Resolve(s.Theme)
	if err != nil {
		return cErr.Render(err.Error())
	}
	overlay := paths.Overlay()
	if overlay == "" {
		return cErr.Render("找不到 gong-overlay")
	}
	cmd := exec.Command(overlay, "--force",
		"--lead", fmt.Sprint(th.LeadSeconds()),
		"--timeout", fmt.Sprint(th.TimeoutSeconds()),
		"--name", "vis", "--theme", th.HTML)
	if err := cmd.Start(); err != nil {
		return cErr.Render(err.Error())
	}
	go cmd.Wait() // 收尸，别留僵尸进程
	return "正在预览 " + th.Label()
}

// ---- 渲染 ----

func (m model) View() string {
	var b strings.Builder
	b.WriteString(cTitle.Render(" gong · 定时") + "\n\n")

	if len(m.cfg.Schedules) == 0 {
		b.WriteString(cDim.Render("  一条定时都没有。按 a 加一条。") + "\n")
	}
	for i, s := range m.cfg.Schedules {
		sel := i == m.cursor
		dot := cOff.Render("○")
		if s.Enabled {
			dot = cOn.Render("●")
		}
		cursor := "  "
		if sel {
			cursor = cAmber.Render("▸ ")
		}
		name := s.Name
		if sel {
			name = cSel.Render(name)
		}
		editing := sel && m.mode == modeEdit

		line := fmt.Sprintf("%s%s %-14s %s  %s  %s",
			cursor, dot, name,
			m.renderClock(s, editing),
			m.renderWeek(s, editing),
			m.renderTheme(s, editing))
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	switch m.mode {
	case modeRename:
		b.WriteString("  改名：" + cField.Render(m.buf+" ") + cDim.Render("  回车确认 · esc 取消") + "\n")
	case modeConfirmDelete:
		if s := m.cur(); s != nil {
			b.WriteString("  " + cErr.Render("删掉 "+s.Name+"？") + cDim.Render("  y 确认 · 其他键取消") + "\n")
		}
	case modeEdit:
		b.WriteString(cKeyCap.Render("  ←→ 选字段 · ↑↓ 改值 · r 改名 · v 预览 · esc 回列表") + "\n")
	default:
		b.WriteString(cKeyCap.Render("  ↑↓ 选择 · space 启停 · e 编辑 · a 新增 · d 删除 · r 改名 · v 预览") + "\n")
		b.WriteString(cKeyCap.Render("  s 保存并接管 launchd · q 退出") + "\n")
	}

	if m.status != "" {
		b.WriteString("\n  " + m.status + "\n")
	}
	if m.mode == modeEdit {
		if s := m.cur(); s != nil {
			if th, err := theme.Resolve(s.Theme); err == nil {
				b.WriteString("\n" + cDim.Render(fmt.Sprintf("  %s — %s（提前 %d 秒亮相，%s）",
					th.Label(), th.Meta.Desc, th.LeadSeconds(), th.Meta.Placement)) + "\n")
			}
		}
	}
	return b.String()
}

func (m model) renderClock(s config.Schedule, editing bool) string {
	secs := s.Seconds()
	parts := []string{
		fmt.Sprintf("%02d", secs/3600),
		fmt.Sprintf("%02d", (secs/60)%60),
		fmt.Sprintf("%02d", secs%60),
	}
	for i := range parts {
		if editing && m.field == fHour+i {
			parts[i] = cField.Render(parts[i])
		}
	}
	return strings.Join(parts, ":")
}

func (m model) renderWeek(s config.Schedule, editing bool) string {
	set := map[int]bool{}
	for _, d := range s.Weekdays {
		set[d%7] = true
	}
	var b strings.Builder
	for i, day := range weekOrder {
		lbl := weekLabel[i]
		if set[day] {
			lbl = cAmber.Render(lbl)
		} else {
			lbl = cDim.Render("·")
		}
		if editing && m.field == fWeek0+i {
			lbl = cField.Render(lbl)
		}
		b.WriteString(lbl)
	}
	return b.String()
}

func (m model) renderTheme(s config.Schedule, editing bool) string {
	label := s.Theme
	if _, err := theme.Resolve(s.Theme); err != nil {
		label = cErr.Render(s.Theme + " ✗")
	} else if editing && m.field == fTheme {
		label = cField.Render(s.Theme)
	}
	return label
}
