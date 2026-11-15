package aiflow

// 模型列表与 embeddings 的 golden 对拍；夹具来自 aiflow-golden-models.test.ts。

import (
	"context"
	"testing"
)

func TestModelsGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string       `json:"name"`
			APIURL   string       `json:"apiUrl"`
			APIKey   string       `json:"apiKey"`
			Protocol *string      `json:"protocol"`
			Script   []scriptStep `json:"script"`
			Calls    int          `json:"calls"`
			Expect   struct {
				Requests []recordedRequest `json:"requests"`
				Results  []map[string]any  `json:"results"`
			} `json:"expect"`
		} `json:"cases"`
		Previews []struct {
			APIURL   string  `json:"apiUrl"`
			Protocol *string `json:"protocol"`
			Expect   string  `json:"expect"`
		} `json:"previews"`
	}
	loadGolden(t, "models", &golden)
	if len(golden.Cases) < 15 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			fake := newFakeTransport(c.Script, nil)
			client := &Client{Transport: fake}
			protocol := ""
			if c.Protocol != nil {
				protocol = *c.Protocol
			}
			var results []map[string]any
			for i := 0; i < c.Calls; i++ {
				r := client.FetchModels(context.Background(), c.APIURL, c.APIKey, protocol)
				models := []map[string]any{}
				for _, m := range r.Models {
					entry := map[string]any{"id": m.ID, "name": m.Name}
					if m.OwnedBy != "" {
						entry["owned_by"] = m.OwnedBy
					}
					if m.Created != nil {
						entry["created"] = *m.Created
					}
					if m.Description != "" {
						entry["description"] = m.Description
					}
					models = append(models, entry)
				}
				res := map[string]any{"success": r.Success, "models": models}
				if r.Error != "" || !r.Success {
					res["error"] = r.Error
				}
				if r.Reason != "" {
					res["reason"] = r.Reason
				}
				if r.Endpoint != "" {
					res["endpoint"] = r.Endpoint
				}
				if r.Status != nil {
					res["status"] = *r.Status
				}
				results = append(results, res)
			}
			assertSame(t, "请求", nonNil(c.Expect.Requests), nonNil(fake.Requests))
			assertSame(t, "结果", c.Expect.Results, results)
		})
	}

	for _, p := range golden.Previews {
		protocol := ""
		if p.Protocol != nil {
			protocol = *p.Protocol
		}
		if got := APIEndpointPreview(p.APIURL, protocol); got != p.Expect {
			t.Errorf("getApiEndpointPreview(%q,%q)：Go=%q TS=%q", p.APIURL, protocol, got, p.Expect)
		}
	}
}

func TestEmbedGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string       `json:"name"`
			Config goldenConfig `json:"config"`
			Texts  []string     `json:"texts"`
			Script []scriptStep `json:"script"`
			Abort  *string      `json:"abort"`
			Calls  int          `json:"calls"`
			Expect struct {
				Requests []recordedRequest `json:"requests"`
				Usage    []usageRecord     `json:"usage"`
				Results  []any             `json:"results"`
				Errors   []any             `json:"errors"`
			} `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "embed", &golden)
	if len(golden.Cases) < 12 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.Abort != nil && *c.Abort == "before" {
				cancel()
			}
			fake := newFakeTransport(c.Script, cancel)
			usage := &usageRecorder{}
			runner := &Runner{Client: &Client{Transport: fake}, Config: staticConfig{}, Usage: usage}

			results, errs := []any{}, []any{}
			for i := 0; i < c.Calls; i++ {
				vectors, err := runner.EmbedTexts(ctx, c.Config.toAIConfig(), c.Texts)
				if err != nil {
					results = append(results, nil)
					errs = append(errs, summarizeError(err))
				} else {
					results = append(results, vectors)
					errs = append(errs, nil)
				}
			}
			assertSame(t, "请求", nonNil(c.Expect.Requests), nonNil(fake.Requests))
			assertSame(t, "用量记录", nonNil(c.Expect.Usage), nonNil(usage.Entries))
			assertSame(t, "向量结果", c.Expect.Results, results)
			assertSame(t, "错误", c.Expect.Errors, errs)
		})
	}
}
