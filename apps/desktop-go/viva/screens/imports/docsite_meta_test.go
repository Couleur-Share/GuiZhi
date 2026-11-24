package imports

import (
	"slices"
	"testing"

	"guizhi-mygo-shell/native/services"
)

func TestJobAndPageStatusLabels(t *testing.T) {
	for _, s := range []string{
		services.CrawlJobPending, services.CrawlJobRunning, services.CrawlJobPaused,
		services.CrawlJobInterrupted, services.CrawlJobCompleted, services.CrawlJobCanceled,
	} {
		if JobStatusLabel(s) == "" {
			t.Errorf("批次状态 %q 没有文案", s)
		}
	}
	for _, s := range []string{
		services.CrawlPagePending, services.CrawlPageRunning, services.CrawlPageAdded, services.CrawlPageDuplicate,
		services.CrawlPageUpdated, services.CrawlPagePendingVersion, services.CrawlPageUnchanged,
		services.CrawlPageFailed, services.CrawlPageSkipped, services.CrawlPageCanceled,
	} {
		if PageStatusLabel(s) == "" {
			t.Errorf("页面状态 %q 没有文案", s)
		}
	}
	if JobStatusLabel("bogus") != "" || PageStatusLabel("bogus") != "" {
		t.Fatal("认不出的状态应返回空串")
	}
}

func TestOrderedCounts(t *testing.T) {
	job := services.CrawlJob{Counts: map[string]int{
		services.CrawlPageFailed: 2, services.CrawlPageAdded: 3, services.CrawlPagePending: 0, "zeta": 1, "alpha": 4,
	}}
	got := OrderedCounts(job)
	var order []string
	for _, e := range got {
		order = append(order, e.Status)
	}
	want := []string{services.CrawlPagePending, services.CrawlPageAdded, services.CrawlPageFailed, "alpha", "zeta"}
	if !slices.Equal(order, want) {
		t.Fatalf("顺序 = %v，期望 %v", order, want)
	}
	if got[0].Count != 0 || got[1].Count != 3 || got[2].Count != 2 || got[3].Count != 4 {
		t.Fatalf("页数不对：%+v", got)
	}
	if got[3].Label != "alpha" || got[1].Label != PageStatusLabel(services.CrawlPageAdded) {
		t.Fatalf("文案不对：%+v", got)
	}
	if n := len(OrderedCounts(services.CrawlJob{})); n != 0 {
		t.Fatalf("没有计数应为空，得到 %d 项", n)
	}
}

func TestActionsFor(t *testing.T) {
	cases := []struct {
		status string
		failed int
		want   JobActions
	}{
		{services.CrawlJobRunning, 0, JobActions{ShowPause: true, ShowCancel: true}},
		{services.CrawlJobPending, 0, JobActions{ShowResume: true, ShowCancel: true}},
		{services.CrawlJobPaused, 1, JobActions{ShowResume: true, ShowCancel: true, CanRetry: true}},
		{services.CrawlJobInterrupted, 0, JobActions{ShowResume: true, ShowCancel: true}},
		{services.CrawlJobCompleted, 2, JobActions{ShowResume: true, CanRetry: true}},
		{services.CrawlJobCanceled, 0, JobActions{}},
	}
	for _, tc := range cases {
		job := services.CrawlJob{Status: tc.status, Counts: map[string]int{}}
		if tc.failed > 0 {
			job.Counts[services.CrawlPageFailed] = tc.failed
		}
		if got := ActionsFor(job); got != tc.want {
			t.Errorf("%s(失败 %d) = %+v，期望 %+v", tc.status, tc.failed, got, tc.want)
		}
	}
}

func TestJobTitleURL(t *testing.T) {
	if got := JobTitleURL(services.CrawlJob{}); got != "" {
		t.Fatalf("没有入口应为空，得到 %q", got)
	}
	job := services.CrawlJob{Input: services.CrawlInput{Seeds: []services.CrawlSeed{{URL: "https://a.com/x/"}, {URL: "https://b.com/"}}}}
	if got := JobTitleURL(job); got != "https://a.com/x/" {
		t.Fatalf("应取第一个入口，得到 %q", got)
	}
}
