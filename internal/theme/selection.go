package theme

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/xwvike/gong/internal/config"
)

// ValidateChoice 同时接受真实主题和定时专用的动态策略。
func ValidateChoice(id string) error {
	if config.IsThemeStrategy(id) {
		if len(List()) == 0 {
			return fmt.Errorf("一个主题都没找到，无法使用%s策略", config.ThemeLabel(id))
		}
		return nil
	}
	_, err := Resolve(id)
	return err
}

// WakeLead 返回 launchd 需要预留的提前量。动态策略必须按最慢主题唤醒，
// 真正播放时再由选中的主题等待到自己的亮相时刻。
func WakeLead(id string) int {
	if !config.IsThemeStrategy(id) {
		if th, err := Resolve(id); err == nil {
			return th.LeadSeconds()
		}
		return 0
	}
	lead := 0
	for _, th := range List() {
		lead = max(lead, th.LeadSeconds())
	}
	return lead
}

// Select 为一条定时的本次目标时刻选出实际主题。
func Select(s config.Schedule, scheduleIndex int, target time.Time) (Theme, error) {
	return SelectFrom(List(), s, scheduleIndex, target)
}

// SelectFrom 允许 TUI 使用启动时已经加载好的主题集，避免预览和保存时
// 因磁盘变化得到两套不同的候选项。
func SelectFrom(candidates []Theme, s config.Schedule, scheduleIndex int, target time.Time) (Theme, error) {
	choices := append([]Theme(nil), candidates...)
	sort.Slice(choices, func(i, j int) bool { return choices[i].ID < choices[j].ID })
	if len(choices) == 0 {
		return Theme{}, fmt.Errorf("一个主题都没找到")
	}

	id := config.NormalizeTheme(s.Theme)
	switch id {
	case config.ThemeRandom:
		return choices[randomIndex(choices, scheduleIndex, target)], nil
	case config.ThemeSequence:
		ordinal := occurrenceOrdinal(s.Weekdays, target) + int64(scheduleIndex)
		return choices[positiveMod(ordinal, len(choices))], nil
	default:
		for _, th := range choices {
			if th.ID == id {
				return th, nil
			}
		}
		return Theme{}, fmt.Errorf("找不到主题 %q", id)
	}
}

func randomIndex(choices []Theme, scheduleIndex int, target time.Time) int {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%d\x00", scheduleIndex, target.Unix())
	for _, th := range choices {
		fmt.Fprintf(h, "%s\x00", th.ID)
	}
	sum := h.Sum(nil)
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(len(choices)))
}

// occurrenceOrdinal 统计固定纪元到目标日期之前实际会触发的次数；只算选中的
// 星期，因此周五之后的下一次周一恰好递增一次。
func occurrenceOrdinal(weekdays []int, target time.Time) int64 {
	set := make(map[int]bool, len(weekdays))
	for _, d := range weekdays {
		set[((d%7)+7)%7] = true
	}
	y, m, d := target.Date()
	targetDay := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	anchor := time.Date(1970, 1, 5, 0, 0, 0, 0, time.UTC) // 周一
	delta := targetDay - anchor.Unix()/86400
	if delta >= 0 {
		return countWeekdays(1, delta, set)
	}
	return -countWeekdays(int(target.Weekday()), -delta, set)
}

func countWeekdays(start int, days int64, selected map[int]bool) int64 {
	weeks, rest := days/7, int(days%7)
	count := weeks * int64(len(selected))
	for i := range rest {
		if selected[(start+i)%7] {
			count++
		}
	}
	return count
}

func positiveMod(v int64, n int) int {
	return int((v%int64(n) + int64(n)) % int64(n))
}
