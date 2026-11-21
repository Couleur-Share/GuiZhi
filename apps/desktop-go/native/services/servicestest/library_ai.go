package servicestest

import (
	"context"
	"slices"

	"guizhi-mygo-shell/native/services"
)

// M2-6c AI 摘要、标签建议与图片 OCR 的假实现；零值可用。
type aiState struct {
	summaryResult services.LibrarySummaryResult
	summarySet    bool
	summaryErr    error
	tagSuggests   []string
	tagSuggestErr error
	ocrTexts      map[string]string
	ocrErrs       map[string]error
	ocrErr        error
	summaryGate   chan struct{}
	ocrGate       chan struct{}
	suggestGate   chan struct{}
	updateGate    chan struct{}
}

// SeedSummary 配置下一次 GenerateSummary 的返回或错误。
func (f *Library) SeedSummary(res services.LibrarySummaryResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ai.summaryResult = res
	f.ai.summarySet = true
	f.ai.summaryErr = err
}

// SeedTagSuggestions 配置 SuggestTags 返回的候选标签或错误。
func (f *Library) SeedTagSuggestions(tags []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ai.tagSuggests = slices.Clone(tags)
	f.ai.tagSuggestErr = err
}

// SeedOcr 配置某张图片（或全部图片）的识别文本或错误。
func (f *Library) SeedOcr(texts map[string]string, errs map[string]error, allErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if texts != nil {
		f.ai.ocrTexts = make(map[string]string, len(texts))
		for k, v := range texts {
			f.ai.ocrTexts[k] = v
		}
	}
	if errs != nil {
		f.ai.ocrErrs = make(map[string]error, len(errs))
		for k, v := range errs {
			f.ai.ocrErrs[k] = v
		}
	}
	f.ai.ocrErr = allErr
}

// HoldSummary 让下一次 GenerateSummary 卡住（在读取种子前）。
func (f *Library) HoldSummary() (release func()) { return f.hold(&f.ai.summaryGate) }

// HoldOcr 让下一次 RecognizeImageText 卡住。
func (f *Library) HoldOcr() (release func()) { return f.hold(&f.ai.ocrGate) }

// HoldSuggestTags 让下一次 SuggestTags 卡住。
func (f *Library) HoldSuggestTags() (release func()) { return f.hold(&f.ai.suggestGate) }

// HoldUpdateItemFields 让下一次 UpdateItemFields 卡住。
func (f *Library) HoldUpdateItemFields() (release func()) { return f.hold(&f.ai.updateGate) }

func (f *Library) GenerateSummary(ctx context.Context, title, content string) (services.LibrarySummaryResult, error) {
	if err := f.begin("GenerateSummary", "GenerateSummary"); err != nil {
		return services.LibrarySummaryResult{}, err
	}
	f.mu.Lock()
	gate := takeGate(&f.ai.summaryGate)
	res, hasSet, err := f.ai.summaryResult, f.ai.summarySet, f.ai.summaryErr
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibrarySummaryResult{}, err
	}
	if err != nil {
		return services.LibrarySummaryResult{}, err
	}
	if !hasSet {
		return services.LibrarySummaryResult{Text: "AI summary: " + title}, nil
	}
	return res, nil
}

func (f *Library) SuggestTags(ctx context.Context, title, content string) ([]string, error) {
	if err := f.begin("SuggestTags", "SuggestTags"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	gate := takeGate(&f.ai.suggestGate)
	tags, err := slices.Clone(f.ai.tagSuggests), f.ai.tagSuggestErr
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func (f *Library) RecognizeImageText(ctx context.Context, assetFileName string) (string, error) {
	if err := f.begin("RecognizeImageText:"+assetFileName, "RecognizeImageText"); err != nil {
		return "", err
	}
	f.mu.Lock()
	gate := takeGate(&f.ai.ocrGate)
	perErr := f.ai.ocrErrs[assetFileName]
	allErr := f.ai.ocrErr
	text, ok := f.ai.ocrTexts[assetFileName]
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return "", err
	}
	if allErr != nil {
		return "", allErr
	}
	if perErr != nil {
		return "", perErr
	}
	if !ok {
		return "OCR text: " + assetFileName, nil
	}
	return text, nil
}

func (f *Library) UpdateItemFields(ctx context.Context, id string, patch services.LibraryItemFieldsPatch) (services.LibraryItem, bool, error) {
	if err := f.begin("UpdateItemFields:"+id, "UpdateItemFields"); err != nil {
		return services.LibraryItem{}, false, err
	}
	f.mu.Lock()
	gate := takeGate(&f.ai.updateGate)
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibraryItem{}, false, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	idx := -1
	for i := range f.entries {
		if f.entries[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return services.LibraryItem{}, false, nil
	}
	e := &f.entries[idx]
	now := f.nowMs()
	e.UpdatedAt = now
	if patch.Content != nil {
		if f.detail.bodies == nil {
			f.detail.bodies = map[string]DetailBody{}
		}
		b := f.detail.bodies[id]
		b.Content = *patch.Content
		f.detail.bodies[id] = b
		e.Snippet = *patch.Content
	}
	if patch.Summary != nil {
		if f.detail.bodies == nil {
			f.detail.bodies = map[string]DetailBody{}
		}
		b := f.detail.bodies[id]
		b.Summary = *patch.Summary
		f.detail.bodies[id] = b
	}
	b := f.detail.bodies[id]
	return services.LibraryItem{
		ID: e.ID, Title: e.Title, Content: b.Content, Summary: b.Summary, Transcript: b.Transcript,
		ItemType: e.ItemType, Status: e.Status, CollectionID: e.CollectionID,
		IsFavorite: e.IsFavorite, IsPinned: e.IsPinned, ReviewStatus: e.ReviewStatus, ReviewReasons: slices.Clone(e.ReviewReasons),
		SourceURI: b.SourceURI, DeletedAt: e.DeletedAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Tags: slices.Clone(e.Tags),
	}, true, nil
}
