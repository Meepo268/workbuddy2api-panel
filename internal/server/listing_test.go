package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// testUpstreamWithPolicy 构造带档位策略的上游 client（不触网）。
func testUpstreamWithPolicy(p *upstream.EffortPolicy) *upstream.Client {
	up := upstream.New()
	up.SetEffortPolicy(p)
	return up
}

func TestApplyCapabilityAliases(t *testing.T) {
	entry := map[string]any{"context_length": int64(200000), "max_output_tokens": int64(64000)}
	applyCapabilityAliases(entry, upstream.ModelInfo{SupportsImages: true})
	if entry["context_window"] != int64(200000) || entry["max_input_tokens"] != int64(200000) {
		t.Errorf("上下文别名=%v/%v want 200000", entry["context_window"], entry["max_input_tokens"])
	}
	if entry["max_completion_tokens"] != int64(64000) {
		t.Errorf("max_completion_tokens=%v want 64000", entry["max_completion_tokens"])
	}
	mods, _ := entry["input_modalities"].([]string)
	if len(mods) != 2 || mods[0] != "text" || mods[1] != "image" {
		t.Errorf("input_modalities=%v want [text image]", entry["input_modalities"])
	}
	out, _ := entry["output_modalities"].([]string)
	if len(out) != 1 || out[0] != "text" {
		t.Errorf("output_modalities=%v want [text]", entry["output_modalities"])
	}

	// 无输出上限：不写 max_completion_tokens（不编造 0/负数）。
	only := map[string]any{"context_length": int64(1000)}
	applyCapabilityAliases(only, upstream.ModelInfo{})
	if _, has := only["max_completion_tokens"]; has {
		t.Errorf("无 max_output_tokens 时不应写 max_completion_tokens")
	}
	if mods, _ := only["input_modalities"].([]string); len(mods) != 1 || mods[0] != "text" {
		t.Errorf("纯文本模型 input_modalities=%v want [text]", only["input_modalities"])
	}
}

func TestApplyReasoningListingPolicy(t *testing.T) {
	// 策略启用、默认档 max：生效默认档 = 声明最高档，上游默认档单独保留。
	h := &Handler{cfg: Config{Upstream: testUpstreamWithPolicy(&upstream.EffortPolicy{
		Enabled: true, DefaultEffort: "max", ClientPriority: true, OverrideScope: "level",
	})}}
	entry := map[string]any{}
	mi := upstream.ModelInfo{ID: "demo", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"}
	h.applyReasoningListing(entry, "cn", mi, mi.Efforts, mi.DefaultEffort)

	if got, _ := entry["reasoning_supported_efforts"].([]string); len(got) != 3 {
		t.Errorf("supported=%v want 3 项", entry["reasoning_supported_efforts"])
	}
	if entry["reasoning_default_effort"] != "high" {
		t.Errorf("策略生效默认档=%v want high（声明最高档）", entry["reasoning_default_effort"])
	}
	if entry["reasoning_upstream_default_effort"] != "medium" {
		t.Errorf("上游默认档=%v want medium", entry["reasoning_upstream_default_effort"])
	}
	opts, _ := entry["reasoning_options"].([]map[string]any)
	if len(opts) != 1 || opts[0]["type"] != "effort" {
		t.Errorf("reasoning_options=%v want [{type:effort}]", entry["reasoning_options"])
	}
	if entry["supports_reasoning"] != true {
		t.Errorf("supports_reasoning 应为 true")
	}

	// JSON 形状：档位列表 + OpenRouter/models.dev 风格 reasoning_options。
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"reasoning_supported_efforts":["low","medium","high"]`,
		`"reasoning_options":[{"type":"effort","values":["low","medium","high"]}]`,
		`"reasoning_default_effort":"high"`,
		`"reasoning_upstream_default_effort":"medium"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON 缺 %s：%s", want, raw)
		}
	}
}

func TestApplyReasoningListingPolicyDisabledFallsBackToUpstream(t *testing.T) {
	h := &Handler{cfg: Config{Upstream: testUpstreamWithPolicy(&upstream.EffortPolicy{Enabled: false})}}
	entry := map[string]any{}
	mi := upstream.ModelInfo{ID: "glm-5.1"} // CN 静态表只有 medium、无默认档
	h.applyReasoningListing(entry, "cn", mi, nil, "")
	if _, has := entry["reasoning_default_effort"]; has {
		t.Errorf("策略关闭且上游无默认档时不应写 reasoning_default_effort, got %v", entry["reasoning_default_effort"])
	}
	got, _ := entry["reasoning_supported_efforts"].([]string)
	if len(got) != 1 || got[0] != "medium" {
		t.Errorf("glm-5.1 supported=%v want [medium]", entry["reasoning_supported_efforts"])
	}
}

func TestApplyReasoningListingSingleFixedEffort(t *testing.T) {
	h := &Handler{cfg: Config{Upstream: testUpstreamWithPolicy(nil)}}
	entry := map[string]any{}
	// 目录只给 reasoning.effort=HIGH，没有 supportedEfforts → 声明单档。
	mi := upstream.ModelInfo{ID: "fixed-effort", ReasoningEffort: "HIGH"}
	h.applyReasoningListing(entry, "cn", mi, nil, "")
	got, _ := entry["reasoning_supported_efforts"].([]string)
	if len(got) != 1 || got[0] != "high" {
		t.Errorf("单档声明=%v want [high]", entry["reasoning_supported_efforts"])
	}
	if entry["reasoning_default_effort"] != "high" {
		t.Errorf("单档默认=%v want high", entry["reasoning_default_effort"])
	}
}

func TestApplyReasoningListingUnknownModelLeavesNoReasoningFields(t *testing.T) {
	h := &Handler{cfg: Config{Upstream: testUpstreamWithPolicy(nil)}}
	entry := map[string]any{}
	h.applyReasoningListing(entry, "cn", upstream.ModelInfo{ID: "unknown-model"}, nil, "")
	for _, k := range []string{"reasoning_supported_efforts", "reasoning_options", "reasoning_default_effort", "reasoning_upstream_default_effort"} {
		if _, has := entry[k]; has {
			t.Errorf("未知档位模型不应写 %s, got %v", k, entry[k])
		}
	}
}

func TestApplyReasoningListingGlobalStaticTable(t *testing.T) {
	h := &Handler{cfg: Config{Upstream: testUpstreamWithPolicy(&upstream.EffortPolicy{Enabled: true, DefaultEffort: "max"})}}
	entry := map[string]any{}
	mi := upstream.ModelInfo{ID: "gpt-5.5"}
	h.applyReasoningListing(entry, "global", mi, nil, "")
	got, _ := entry["reasoning_supported_efforts"].([]string)
	if len(got) == 0 {
		t.Fatalf("global 静态表应声明 gpt-5.5 档位")
	}
	if entry["reasoning_default_effort"] != "xhigh" {
		t.Errorf("gpt-5.5 生效默认档=%v want xhigh（最高档）", entry["reasoning_default_effort"])
	}
}
