package tui

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

// fakeThemes 避免测试依赖磁盘主题。
func fakeThemes() []theme.Theme {
	return []theme.Theme{
		{ID: "alpha", HTML: "/dev/null", Meta: theme.Meta{Lead: 0, Duration: 5}},
		{ID: "beta", HTML: "/dev/null", Meta: theme.Meta{Lead: 3, Duration: 8}},
	}
}

// 测试用标签只用于区分断言对象。
func twoSchedules() *config.Config {
	return &config.Config{Version: config.CurrentVersion, Schedules: []config.Schedule{
		{Label: "noon", At: "12:00:00", Weekdays: []int{1, 2, 3, 4, 5}, Theme: "alpha", Enabled: true, Grace: config.DefaultGrace},
		{Label: "evening", At: "18:00:00", Weekdays: []int{1, 2, 3, 4, 5}, Theme: "beta", Enabled: true, Grace: config.DefaultGrace},
	}}
}

func startModel(t *testing.T, c *config.Config) *teatest.TestModel {
	t.Helper()
	m := newModel(c, fakeThemes())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 32))
	t.Cleanup(func() { _ = tm.Quit() })
	waitForRender(t, tm)
	return tm
}

// waitForRender 避免按键早于初始 WindowSizeMsg。
func waitForRender(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("定时"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(5*time.Millisecond))
}

func waitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(substr))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(5*time.Millisecond))
}

func press(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func runes(s string) tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func space() tea.KeyMsg              { return tea.KeyMsg{Type: tea.KeySpace} }

func finalModel(t *testing.T, tm *teatest.TestModel) Model {
	t.Helper()
	tm.Send(tea.QuitMsg{})
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))
	m, ok := fm.(Model)
	if !ok {
		t.Fatalf("FinalModel 不是 tui.Model：%T", fm)
	}
	return m
}

// 直接读取首帧，因为 startModel 会消费包含序号的输出缓冲区。
func TestTableShowsSequenceNumbers(t *testing.T) {
	c := twoSchedules()
	m := newModel(c, fakeThemes())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 32))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("#1")) && bytes.Contains(out, []byte("#2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(5*time.Millisecond))
	finalModel(t, tm)
}

func TestTableHeaderAndCellsUseSameHorizontalPadding(t *testing.T) {
	if styleTableHeader.GetPaddingLeft() != styleTableCell.GetPaddingLeft() ||
		styleTableHeader.GetPaddingRight() != styleTableCell.GetPaddingRight() {
		t.Fatal("表头和内容的水平留白不一致，列会错位")
	}
}

func TestToggleEnabled(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(space())
	m := finalModel(t, tm)

	if c.Schedules[0].Enabled {
		t.Error("space 应该把第一条定时停用")
	}
	if !m.changed {
		t.Error("停用之后 changed 应该是 true")
	}
}

// 新增不需要取名字：标签留空完全合法，序号自己就是身份。
func TestAddAppendsScheduleWithoutRequiringLabel(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("a"))
	m := finalModel(t, tm)

	if len(c.Schedules) != 3 {
		t.Fatalf("应该新增一条，现在有 %d 条", len(c.Schedules))
	}
	if got := c.Schedules[2].Label; got != "" {
		t.Errorf("新定时不该自带标签，got %q", got)
	}
	if m.mode != modeEdit {
		t.Error("新增之后应该直接进编辑态")
	}
	if !m.changed {
		t.Error("新增之后 changed 应该是 true")
	}
}

func TestEditLabelSetsLabel(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("r"))
	tm.Send(press(tea.KeyCtrlU)) // 清空输入框内容（textinput 默认支持 ctrl+u）
	tm.Send(runes("lunch"))
	tm.Send(press(tea.KeyEnter))
	m := finalModel(t, tm)

	if c.Schedules[0].Label != "lunch" {
		t.Errorf("标签 = %q，想要 lunch", c.Schedules[0].Label)
	}
	if m.mode != modeList {
		t.Error("从列表进的改标签，提交后应该回到列表态")
	}
	if !m.changed {
		t.Error("改标签后 changed 应该是 true")
	}
}

// 标签是纯装饰：带空格、跟别的定时重复、留空，一律合法，不该被拒。
func TestEditLabelAllowsAnyLabelIncludingDuplicatesAndSpaces(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("r"))
	tm.Send(press(tea.KeyCtrlU))
	tm.Send(runes("evening")) // 跟第二条重复也没关系
	tm.Send(press(tea.KeyEnter))
	m := finalModel(t, tm)

	if c.Schedules[0].Label != "evening" {
		t.Errorf("重复的标签应该被接受，got %q", c.Schedules[0].Label)
	}
	if m.mode != modeList {
		t.Error("提交后应该回到列表态，不该被挡在改标签框里")
	}
}

// 提交空标签等于清空——标签本来就不是必填项。
func TestEditLabelToEmptyClearsLabel(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("r"))
	tm.Send(press(tea.KeyCtrlU))
	tm.Send(press(tea.KeyEnter))
	finalModel(t, tm)

	if c.Schedules[0].Label != "" {
		t.Errorf("提交空输入应该清空标签，got %q", c.Schedules[0].Label)
	}
}

func TestEditFieldStepping(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter)) // e/enter 进编辑态，光标在第一行 noon
	tm.Send(press(tea.KeyUp))    // 默认字段是小时，+1
	tm.Send(press(tea.KeyRight)) // 切到分钟
	tm.Send(press(tea.KeyUp))
	tm.Send(press(tea.KeyUp))
	m := finalModel(t, tm)

	if got := c.Schedules[0].At; got != "13:02:00" {
		t.Errorf("时间 = %s，想要 13:02:00", got)
	}
	if !m.changed {
		t.Error("调整字段后 changed 应该是 true")
	}
}

func TestHourWrapsAtBoundary(t *testing.T) {
	c := twoSchedules()
	c.Schedules[0].At = "23:00:00"
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter))
	tm.Send(press(tea.KeyUp)) // 23 + 1 应该回绕到 0，不是 24
	finalModel(t, tm)

	if got := c.Schedules[0].At; got != "00:00:00" {
		t.Errorf("时间 = %s，想要回绕到 00:00:00", got)
	}
}

func TestGraceStepClampsAtZero(t *testing.T) {
	c := twoSchedules()
	c.Schedules[0].Grace = 60 // 1 分钟
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter))
	// 字段顺序：时 分 秒 宽限 主题 周一..周日，宽限是第 4 个，右移 3 次到达
	tm.Send(press(tea.KeyRight))
	tm.Send(press(tea.KeyRight))
	tm.Send(press(tea.KeyRight))
	tm.Send(press(tea.KeyDown)) // 1 分钟 - 1 = 0
	tm.Send(press(tea.KeyDown)) // 不能再往下，必须停在 0
	finalModel(t, tm)

	if c.Schedules[0].Grace != 0 {
		t.Errorf("Grace = %d 秒，想要被夹在 0", c.Schedules[0].Grace)
	}
}

func TestThemePickRoundTrip(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter)) // 进编辑态，选中 noon（当前主题 alpha）
	tm.Send(runes("t"))          // 打开主题库
	tm.Send(press(tea.KeyDown))  // alpha -> beta
	tm.Send(press(tea.KeyEnter)) // 确认选中
	m := finalModel(t, tm)

	if c.Schedules[0].Theme != "beta" {
		t.Errorf("主题 = %q，想要 beta", c.Schedules[0].Theme)
	}
	if m.tab != tabSchedules || m.mode != modeEdit {
		t.Errorf("选完应该回到定时 tab 的编辑态，现在 tab=%v mode=%v", m.tab, m.mode)
	}
	if !m.changed {
		t.Error("选主题后 changed 应该是 true")
	}
}

func TestThemePickerOffersRandomAndSequence(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter))
	tm.Send(runes("t"))
	tm.Send(press(tea.KeyUp)) // alpha -> 顺序
	tm.Send(press(tea.KeyUp)) // 顺序 -> 随机
	tm.Send(press(tea.KeyEnter))
	m := finalModel(t, tm)

	if got := c.Schedules[0].Theme; got != config.ThemeRandom {
		t.Fatalf("选择随机后主题 = %q", got)
	}
	if len(m.list.Items()) != len(fakeThemes()) {
		t.Fatal("退出定时主题选择后，主题库不应继续显示策略项")
	}
}

func TestThemeStrategyDoesNotOfferPreview(t *testing.T) {
	m := newModel(twoSchedules(), fakeThemes())
	m.pickingTheme = true
	m.tab = tabThemes
	m.setThemePickerItems(true)
	m.list.Select(0) // 随机

	for _, binding := range m.currentHelp().ShortHelp() {
		if binding.Help().Key == "v" {
			t.Fatal("随机策略不应展示预览帮助")
		}
	}
	model, cmd := m.updateThemes(runes("v"))
	m = model.(Model)
	if cmd != nil || m.status != "" {
		t.Fatalf("随机策略按 v 应安静忽略：cmd=%v status=%q", cmd, m.status)
	}

	m.list.Select(2) // alpha
	found := false
	for _, binding := range m.currentHelp().ShortHelp() {
		found = found || binding.Help().Key == "v"
	}
	if !found {
		t.Fatal("真实主题仍应展示预览帮助")
	}
}

func TestThemeFieldStepIncludesStrategies(t *testing.T) {
	c := twoSchedules()
	m := newModel(c, fakeThemes())
	m.field = fTheme
	m.bump(&c.Schedules[0], -1) // alpha 前一项是顺序
	if got := c.Schedules[0].Theme; got != config.ThemeSequence {
		t.Fatalf("主题字段没有步进到顺序策略，拿到 %q", got)
	}
}

func TestThemePickCancelKeepsOriginalTheme(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(press(tea.KeyEnter))
	tm.Send(runes("t"))
	tm.Send(press(tea.KeyDown))
	tm.Send(press(tea.KeyEsc)) // 取消，不该改主题
	m := finalModel(t, tm)

	if c.Schedules[0].Theme != "alpha" {
		t.Errorf("取消选主题后主题被改成了 %q", c.Schedules[0].Theme)
	}
	if m.pickingTheme {
		t.Error("取消后 pickingTheme 应该清掉")
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("d"))
	tm.Send(runes("n")) // 任意非 y 键取消
	m := finalModel(t, tm)

	if len(c.Schedules) != 2 {
		t.Error("按非 y 键不该真的删除")
	}
	if m.mode != modeList {
		t.Error("取消删除后应该回到列表态")
	}
}

func TestDeleteConfirmed(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("d"))
	tm.Send(runes("y"))
	m := finalModel(t, tm)

	if len(c.Schedules) != 1 {
		t.Fatalf("应该只剩 1 条，现在 %d 条", len(c.Schedules))
	}
	if c.Schedules[0].Label != "evening" {
		t.Error("删掉的应该是被选中的第一条 noon，留下 evening")
	}
	if !m.changed {
		t.Error("删除后 changed 应该是 true")
	}
}

// 身份是位置，不是持久 ID：删掉 #1 之后，原来的 #2 就该显示成新的 #1。
func TestDeleteRenumbersRemaining(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(runes("d"))
	tm.Send(runes("y"))
	finalModel(t, tm)

	if len(c.Schedules) != 1 {
		t.Fatalf("应该只剩 1 条，现在 %d 条", len(c.Schedules))
	}
	if got := c.Schedules[0].Ref(0); got != "evening" {
		t.Errorf("剩下的那条应该在位置 0，Ref = %q", got)
	}
}

func TestQuitRequiresSecondPressAfterUnsavedChange(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(space()) // 制造一个未保存的改动
	tm.Send(runes("q"))
	waitForOutput(t, tm, "未保存的改动")

	// 光看提示还不够：必须确认程序这时候真的还活着，第一下 q 不能就退出。
	tm.Send(runes("a")) // 如果程序已经退出，这次发送会被忽略；如果还活着，会新增一条
	m1 := finalModel(t, tm)
	if len(c.Schedules) != 3 {
		t.Fatal("第一次 q 之后程序应该还在运行，能继续交互")
	}
	_ = m1

	// 第二个用例单独验证「真的按两次 q 会退出」，走一个全新的会话。
	c2 := twoSchedules()
	tm2 := startModel(t, c2)
	tm2.Send(space())
	tm2.Send(runes("q"))
	waitForOutput(t, tm2, "未保存的改动")
	tm2.Send(runes("q"))
	tm2.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestSaveStillWorksAfterQuitWarning(t *testing.T) {
	c := twoSchedules()
	tm := startModel(t, c)

	tm.Send(space())
	tm.Send(runes("q"))
	waitForOutput(t, tm, "未保存的改动")
	tm.Send(runes("s"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))
	m := fm.(Model)
	if !m.save || !m.changed {
		t.Fatalf("q 后保存丢失状态：save=%v changed=%v", m.save, m.changed)
	}
}

func TestQuitWarningRearmsAfterAnotherAction(t *testing.T) {
	m := newModel(twoSchedules(), fakeThemes())
	m.changed = true

	model, cmd := m.updateKey(runes("q"))
	m = model.(Model)
	if cmd != nil || !m.quitArmed {
		t.Fatalf("第一次 q 应进入待确认状态：cmd=%v armed=%v", cmd, m.quitArmed)
	}

	model, _ = m.updateKey(runes("?"))
	m = model.(Model)
	if m.quitArmed {
		t.Fatal("继续操作后应取消退出确认")
	}

	model, cmd = m.updateKey(runes("q"))
	m = model.(Model)
	if cmd != nil || !m.quitArmed {
		t.Fatalf("继续操作后的 q 应重新警告：cmd=%v armed=%v", cmd, m.quitArmed)
	}
}

func TestSaveRejectsConflictingTriggerSlots(t *testing.T) {
	c := twoSchedules()
	c.Schedules[1].At = "12:00:30"
	c.Schedules[1].Theme = "alpha"
	tm := startModel(t, c)

	tm.Send(runes("s"))
	waitForOutput(t, tm, "触发时间冲突")
	finalModel(t, tm)
}

func TestSaveFailsValidationWithoutQuitting(t *testing.T) {
	c := twoSchedules()
	c.Schedules[0].Weekdays = nil // 手工构造一条校验会挂的配置
	tm := startModel(t, c)

	tm.Send(runes("s"))
	waitForOutput(t, tm, "存不了")

	// 校验失败不能退出：程序还活着的话，接下来这次 space 应该照常生效。
	tm.Send(space())
	finalModel(t, tm)
	if c.Schedules[0].Enabled {
		t.Fatal("校验失败后程序应该还在运行；如果它已经退出，这次 toggle 不会生效")
	}
}

// 新定时应优先使用默认主题，而非字母序第一项。
func TestAddUsesDefaultThemeNotAlphabeticalFirst(t *testing.T) {
	withDefault := append(fakeThemes(), theme.Theme{
		ID: config.DefaultTheme, HTML: "/dev/null",
		Meta: theme.Meta{Duration: 5},
	})
	c := twoSchedules()

	// 覆盖真实新增路径，而非只测辅助函数。
	tm := teatest.NewTestModel(t, newModel(c, withDefault),
		teatest.WithInitialTermSize(100, 32))
	t.Cleanup(func() { _ = tm.Quit() })
	waitForRender(t, tm)
	tm.Send(runes("a"))
	finalModel(t, tm)

	if len(c.Schedules) != 3 {
		t.Fatalf("应该新增一条，现在有 %d 条", len(c.Schedules))
	}
	if got := c.Schedules[2].Theme; got != config.DefaultTheme {
		t.Errorf("新定时的主题 = %q，想要 %q（alpha 说明又回到了按字母序取第一个）",
			got, config.DefaultTheme)
	}
}

// 内置目录整个没装好时 led 解析不开，这时候必须还能建出定时来，
// 不能因为拿不到 led 就崩或者留个空主题。
func TestDefaultThemeIDFallsBackWhenDefaultMissing(t *testing.T) {
	m := newModel(twoSchedules(), fakeThemes()) // 里面没有 led
	if got := m.defaultThemeID(); got != "alpha" {
		t.Errorf("没有 led 时该回落到列表第一个，得到 %q", got)
	}
}
