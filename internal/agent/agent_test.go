package agent

import (
	"reflect"
	"testing"
)

func TestComputeTrigger(t *testing.T) {
	workdays := []int{1, 2, 3, 4, 5}
	cases := []struct {
		name     string
		at       int // 秒
		lead     int
		weekdays []int
		wantH    int
		wantM    int
		wantDays []int
	}{
		{"整点无提前", 12 * 3600, 0, workdays, 12, 0, workdays},
		{"提前 5 秒要退到上一分钟", 12 * 3600, 5, workdays, 11, 59, workdays},
		{"提前 60 秒正好退一分钟", 12 * 3600, 60, workdays, 11, 59, workdays},
		{"非整点向下取整", 12*3600 + 30*60 + 40, 5, workdays, 12, 30, workdays},
		// 这条是真会咬人的：午夜前后提前，星期要跟着退一天
		{"跨午夜要把星期往前移", 2, 5, []int{1}, 23, 59, []int{0}},
		{"周日跨午夜回到周六", 0, 10, []int{0}, 23, 59, []int{6}},
		{"周日用 7 表示也认", 12 * 3600, 0, []int{7}, 12, 0, []int{0}},
		{"重复的星期要去重", 9 * 3600, 0, []int{1, 1, 8}, 9, 0, []int{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeTrigger(c.at, c.lead, c.weekdays)
			if got.Hour != c.wantH || got.Minute != c.wantM {
				t.Errorf("时刻 = %02d:%02d，想要 %02d:%02d", got.Hour, got.Minute, c.wantH, c.wantM)
			}
			if !reflect.DeepEqual(got.Weekdays, c.wantDays) {
				t.Errorf("星期 = %v，想要 %v", got.Weekdays, c.wantDays)
			}
		})
	}
}
