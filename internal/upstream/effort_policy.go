package upstream

import "strings"

// EffortPolicy 是网关侧的思维档位策略（配置节 thinking）。
//
// 目标语义（与 README / 面板文案一致）：
//   - 客户端没带档位：无论谁优先，都由网关补齐生效档位；
//   - 客户端带了档位：ClientPriority=true 时客户端优先，false 时网关优先；
//   - 客户端显式关闭思考：reasoning_effort=off/none 属于档位字段，网关优先时照常
//     覆盖；thinking.type=disabled 属于开关，OverrideScope=level 尊重客户端关闭，
//     OverrideScope=all 才连开关一起覆盖（仅改请求里已有的 thinking 对象）。
//
// 生效档位由 ResolveGatewayEffort 计算：DefaultEffort 为空或 "max" 取模型声明档位
// 里的最高档，具体档位不在声明内时同样退化为最高档。
type EffortPolicy struct {
	// Enabled 网关是否补档/覆盖；false 时完全不介入。
	Enabled bool
	// DefaultEffort 网关默认档；空或 "max" = 模型声明档位里的最高档。
	DefaultEffort string
	// ClientPriority true = 客户端显式档位优先；false = 网关优先。
	ClientPriority bool
	// OverrideScope 网关优先时的覆盖范围：level（只覆盖档位）或 all（档位+开关）。
	OverrideScope string
}

const (
	// OverrideScopeLevel 只覆盖档位，客户端显式关闭思考时不动开关。
	OverrideScopeLevel = "level"
	// OverrideScopeAll 连思考开关一起覆盖（客户端 disabled 时改为 enabled）。
	OverrideScopeAll = "all"
)

// normalized 归一化策略字段，空值落到安全默认：level 覆盖、默认档 max。
func (p EffortPolicy) normalized() EffortPolicy {
	p.DefaultEffort = strings.ToLower(strings.TrimSpace(p.DefaultEffort))
	if p.DefaultEffort == "" {
		p.DefaultEffort = "max"
	}
	p.OverrideScope = strings.ToLower(strings.TrimSpace(p.OverrideScope))
	if p.OverrideScope != OverrideScopeAll {
		p.OverrideScope = OverrideScopeLevel
	}
	return p
}

// Normalized 返回归一化后的策略副本（空默认档 → max，未知覆盖范围 → level）。
// 展示侧（/v1/models 声明）与出站侧共用同一份归一化结果，避免两处口径漂移。
func (p EffortPolicy) Normalized() EffortPolicy { return p.normalized() }

// effortField 返回请求体里已有的档位字段名与字符串值。
// snake_case 优先于 camelCase（与 normalizeReasoningEffort 的读取顺序一致）；
// 非字符串值按「已携带但值未知」处理（present=true, val=""），不静默覆盖客户端原值。
func effortField(obj map[string]any) (key, val string, present bool) {
	if v, ok := obj["reasoning_effort"]; ok {
		s, _ := v.(string)
		return "reasoning_effort", s, true
	}
	if v, ok := obj["reasoningEffort"]; ok {
		s, _ := v.(string)
		return "reasoningEffort", s, true
	}
	return "", "", false
}

// isEffortOff 判断客户端是否用档位字段表达了「关闭思考」。
func isEffortOff(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "off", "none", "disabled":
		return true
	}
	return false
}

// thinkingDisabled 客户端是否显式 thinking.type=disabled。
func thinkingDisabled(obj map[string]any) bool {
	t, ok := obj["thinking"].(map[string]any)
	if !ok {
		return false
	}
	typ, _ := t["type"].(string)
	return strings.EqualFold(strings.TrimSpace(typ), "disabled")
}

// applyEffortPolicy 按网关策略写入生效档位。必须在 injectThinking /
// normalizeReasoningEffort 之前调用：前者只补 deepseek 的默认档、后者只管降级，
// 都不负责「客户端缺档/优先级/覆盖关闭」这三类决定。
//
// 只处理模型确实声明了档位（efforts[model] 非空）的请求——没有声明就绝不猜档位，
// 也没有可写入的合法值。
func applyEffortPolicy(obj map[string]any, model string, efforts map[string][]string, policy *EffortPolicy) {
	if policy == nil || !policy.Enabled || len(efforts) == 0 {
		return
	}
	supported := efforts[model]
	if len(supported) == 0 {
		return
	}
	p := policy.normalized()
	target, ok := ResolveGatewayEffort(p.DefaultEffort, supported)
	if !ok {
		return
	}

	key, current, present := effortField(obj)
	off := present && isEffortOff(current)
	disabled := thinkingDisabled(obj)

	// 客户端表达关闭：开关优先于档位。
	if off || disabled {
		if p.ClientPriority {
			return
		}
		// thinking.type=disabled 是开关意图：scope=level 时不碰它（也不补档），
		// 只有 scope=all 才连开关一起覆盖。reasoning_effort=off 属于档位字段，
		// 网关优先时照常覆盖（scope 不影响它）。
		if disabled && p.OverrideScope != OverrideScopeAll {
			return
		}
		if disabled {
			// 只翻转请求里已有的 thinking 对象，不凭空新增（非 deepseek 上游可能不认）。
			if t, ok := obj["thinking"].(map[string]any); ok {
				t["type"] = "enabled"
			}
		}
		if key == "" {
			key = "reasoning_effort"
		}
		obj[key] = target
		return
	}

	// 客户端显式档位：客户端优先则原样透传；网关优先则覆盖。
	if present {
		if p.ClientPriority {
			return
		}
		obj[key] = target
		return
	}

	// 客户端完全没带档位：无论谁优先都补齐。
	obj["reasoning_effort"] = target
}
