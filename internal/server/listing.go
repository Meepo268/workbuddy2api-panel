// listing.go 组装 /v1/models 单条模型的能力声明：推理档位 + 跨客户端通用别名。
//
// 目标：客户端「添加模型」时自动填入正确参数，而不是靠猜。字段分两类——
//   - canonical：网关自己的 reasoning_supported_efforts / reasoning_default_effort，
//     面板与既有客户端在用，保持不变；
//   - 别名：max_input_tokens / max_completion_tokens / context_window /
//     input_modalities / output_modalities / reasoning_options，覆盖主流客户端
//     （DSH、OpenAI 兼容前端、models.dev 风格读取方）的自动发现字段。
//
// 只声明确定值：模型没有档位能力就不写档位字段，没有输出上限就不写 max_completion_tokens。
package server

import (
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// applyCapabilityAliases 从已写好的 canonical 字段派生通用别名（幂等、零额外请求）。
func applyCapabilityAliases(entry map[string]any, mi upstream.ModelInfo) {
	if cl, ok := entry["context_length"].(int64); ok && cl > 0 {
		entry["context_window"] = cl
		entry["max_input_tokens"] = cl
	}
	if mo, ok := entry["max_output_tokens"].(int64); ok && mo > 0 {
		entry["max_completion_tokens"] = mo
	}
	mods := []string{"text"}
	if mi.SupportsImages {
		mods = append(mods, "image")
	}
	entry["input_modalities"] = mods
	entry["output_modalities"] = []string{"text"}
}

// applyReasoningListing 写推理档位声明：
//
//	reasoning_supported_efforts    可选档位（canonical）
//	reasoning_options              [{type:"effort", values:[...]}]（models.dev 风格）
//	reasoning_default_effort       网关实际会补的默认档（受 thinking 策略影响）
//	reasoning_upstream_default_effort 上游/静态表声明的默认档（诊断用，可能与上面不同）
//	supports_reasoning             有档位即视为支持推理（补齐旗标缺失的模型）
//
// effort 数据源与请求侧同源（EffortListing：远端 supportedEfforts 权威 → realm 静态
// 兜底表）；目录只给固定档 reasoning.effort 时退化为单档模型。无档位 → 一个字段都不写。
func (h *Handler) applyReasoningListing(entry map[string]any, realm string, mi upstream.ModelInfo, remoteEfforts []string, remoteDefault string) {
	efforts, upstreamDefault := upstream.EffortListing(realm, mi.ID, remoteEfforts, remoteDefault)
	if len(efforts) == 0 && upstream.IsEffortLevel(mi.ReasoningEffort) {
		// 目录只有 reasoning.effort（固定档）而没有 supportedEfforts：声明成单档，
		// 客户端至少知道「该模型能开思考」以及该发哪个档，不会盲发别的档。
		single := strings.ToLower(strings.TrimSpace(mi.ReasoningEffort))
		efforts = []string{single}
		upstreamDefault = single
	}
	if len(efforts) == 0 {
		return
	}

	entry["reasoning_supported_efforts"] = efforts
	entry["reasoning_options"] = []map[string]any{{"type": "effort", "values": efforts}}
	if upstreamDefault != "" {
		entry["reasoning_upstream_default_effort"] = upstreamDefault
	}

	// 网关默认档 = 策略生效档（空/不支持时退化为声明最高档）；策略关闭时沿用上游默认档。
	effective := upstreamDefault
	if p := h.effortPolicy(); p != nil && p.Enabled {
		np := p.Normalized()
		if resolved, ok := upstream.ResolveGatewayEffort(np.DefaultEffort, efforts); ok {
			effective = resolved
		}
	}
	if effective != "" {
		entry["reasoning_default_effort"] = effective
	}
	entry["supports_reasoning"] = true
}

// effortPolicy 当前网关档位策略；未配置/上游未装配 → nil。
func (h *Handler) effortPolicy() *upstream.EffortPolicy {
	if h.cfg.Upstream == nil {
		return nil
	}
	return h.cfg.Upstream.EffortPolicySnapshot()
}
