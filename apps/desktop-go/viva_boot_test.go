package main

import "testing"

// -viva 在切换为产品默认之前必须被锁在隔离验证里：缺任何一项都要在碰数据之前拒绝。
func TestCheckVivaLaunch(t *testing.T) {
	cases := []struct {
		name                                  string
		nativeOn, bench, dbGiven, userDataGiv bool
		wantErr                               bool
	}{
		{"隔离验证：bench + 显式 db + 显式 userdata", false, true, true, true, false},
		{"缺 -bench", false, false, true, true, true},
		{"缺 -db（会按正式安装规则推断真实数据库）", false, true, false, true, true},
		{"缺 -userdata", false, true, true, false, true},
		{"什么都没给", false, false, false, false, true},
		{"与 -native 同用", true, true, true, true, true},
	}
	for _, c := range cases {
		err := checkVivaLaunch(c.nativeOn, c.bench, c.dbGiven, c.userDataGiv)
		if (err != nil) != c.wantErr {
			t.Errorf("%s：err=%v，期望出错=%v", c.name, err, c.wantErr)
		}
	}
}
