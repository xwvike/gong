package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.renderTabs() + "\n\n")

	switch m.tab {
	case tabThemes:
		b.WriteString(m.list.View())
	default:
		b.WriteString(m.table.View())
	}
	b.WriteString("\n")

	if warnings := m.brokenThemeWarnings(); len(warnings) > 0 && m.tab == tabSchedules {
		for _, w := range warnings {
			b.WriteString(styleErr.Render("  ⚠ "+w) + "\n")
		}
	}

	if m.tab == tabSchedules && m.mode == modeEdit {
		b.WriteString(m.renderEditPanel() + "\n")
	}

	if m.mode == modeLabel {
		b.WriteString("\n  " + m.input.View() + "\n")
	}
	if m.mode == modeConfirmDelete {
		if s := m.selected(); s != nil {
			b.WriteString("\n  " + styleErr.Render("删掉 "+s.Ref(m.table.Cursor())+"？") +
				styleDim.Render("  y 确认 · 其他键取消") + "\n")
		}
	}

	if m.status != "" {
		style := styleTitle
		if m.isErr {
			style = styleErr
		}
		b.WriteString("\n  " + style.Render(m.status) + "\n")
	}

	b.WriteString("\n" + m.help.View(m.currentHelp()))
	return b.String()
}

func (m Model) renderTabs() string {
	labels := []string{"定时", "主题库"}
	var parts []string
	for i, l := range labels {
		style := styleTabOff
		if appTab(i) == m.tab {
			style = styleTabOn
		}
		parts = append(parts, style.Render(l))
	}
	if m.pickingTheme {
		s := m.selected().Ref(m.table.Cursor())
		parts = append(parts, styleDim.Render("  ← 正在为 "+s+" 选主题，enter 确定 · esc 取消"))
	}
	return " " + strings.Join(parts, " ")
}

func (m Model) brokenThemeWarnings() []string {
	var warnings []string
	for i, s := range m.cfg.Schedules {
		if _, err := theme.Resolve(s.Theme); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s 的主题 %q 找不到", s.Ref(i), s.Theme))
		}
	}
	return warnings
}

func (m Model) renderEditPanel() string {
	s := m.selected()
	if s == nil {
		return ""
	}
	label := s.Label
	if label == "" {
		label = styleDim.Render("（无，可选）")
	}
	var b strings.Builder
	b.WriteString(styleLabel.Render("标签") + " " + label + styleDim.Render("  (r 设标签)") + "\n")
	b.WriteString(styleLabel.Render("时间") + " " + m.renderClock(*s) + "\n")
	b.WriteString(styleLabel.Render("宽限") + " " + m.renderGrace(*s) + styleDim.Render("  睡醒补跑的容忍窗口") + "\n")
	b.WriteString(styleLabel.Render("主题") + " " + m.renderThemeField(*s) + styleDim.Render("  (t 打开主题库)") + "\n")
	b.WriteString(styleLabel.Render("星期") + " " + m.renderWeek(*s))

	th, err := theme.Resolve(s.Theme)
	detail := ""
	if err == nil {
		detail = th.ID
		if th.Meta.Desc != "" {
			detail += " — " + th.Meta.Desc
		}
	}
	panel := stylePanel.Render(b.String())
	if detail != "" {
		panel += "\n" + styleDim.Render("  "+detail)
	}
	return panel
}

func (m Model) renderClock(s config.Schedule) string {
	secs := s.Seconds()
	parts := []string{
		fmt.Sprintf("%02d", secs/3600),
		fmt.Sprintf("%02d", (secs/60)%60),
		fmt.Sprintf("%02d", secs%60),
	}
	for i := range parts {
		if m.field == fHour+field(i) {
			parts[i] = styleField.Render(parts[i])
		}
	}
	return strings.Join(parts, ":")
}

func (m Model) renderGrace(s config.Schedule) string {
	text := fmt.Sprintf("%d 分钟", s.Grace/60)
	if m.field == fGrace {
		return styleField.Render(text)
	}
	return text
}

func (m Model) renderThemeField(s config.Schedule) string {
	label := s.Theme
	if _, err := theme.Resolve(s.Theme); err != nil {
		label = styleErr.Render(s.Theme + " ✗ 找不到")
	} else if m.field == fTheme {
		label = styleField.Render(s.Theme)
	}
	return label
}

func (m Model) renderWeek(s config.Schedule) string {
	set := map[int]bool{}
	for _, d := range s.Weekdays {
		set[d%7] = true
	}
	var b strings.Builder
	for i, day := range weekOrder {
		lbl := weekLabel[i]
		if set[day] {
			lbl = styleOn.Render(lbl)
		} else {
			lbl = styleDim.Render("·")
		}
		if m.field == fWeek0+field(i) {
			lbl = styleField.Render(lbl)
		}
		b.WriteString(lbl)
	}
	return b.String()
}

// currentHelp 按当前模式现造一份 help.KeyMap，而不是让 keyMap 自己判断此刻该显示什么。
func (m Model) currentHelp() contextHelp {
	k := m.keys
	switch {
	case m.mode == modeLabel:
		return short(k.Confirm, k.Cancel)
	case m.mode == modeConfirmDelete:
		return short(k.Confirm, k.Cancel)
	case m.tab == tabSchedules && m.mode == modeEdit:
		return full(
			[]key.Binding{k.FieldLeft, k.ValueUp, k.PickTheme, k.EditLabel, k.Preview, k.Back},
			[]key.Binding{k.Save, k.Quit, k.Help},
		)
	case m.tab == tabThemes:
		if m.pickingTheme {
			return short(k.Confirm, k.Cancel, k.Preview)
		}
		return full(
			[]key.Binding{k.TabNext, k.Preview},
			[]key.Binding{k.Save, k.Quit, k.Help},
		)
	default:
		return full(
			[]key.Binding{k.Toggle, k.Edit, k.Add, k.Delete, k.EditLabel, k.Preview, k.TabNext},
			[]key.Binding{k.Save, k.Quit, k.Help},
		)
	}
}
