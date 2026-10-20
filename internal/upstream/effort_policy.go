package upstream

import "strings"

// EffortPolicy 是网关侧的思维档位策略（配置节 thinking）。
//
// 目标语义（与 README / 面板文案一致）：
//   - 客户端没带档位：无论谁优先，都由网关补齐生效档位；
//   - 客户端带了档位：ClientPriority=true 时客户端优先，false 时网关优先；
//   - 客户端显式关闭思考：thinking.type=disabled 与 reasoning_effort=off/none 都算
//     关闭意图。客户端优先、或网关优先且 OverrideScope=level 时尊重关闭（清掉档位，
//     deepseek 补 thinking.type=disabled）；OverrideScope=all 才连开关一起翻开。
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
	// ModelDefaults 每模型覆盖默认档：键 "cn:model"/"global:model"，裸模型名也接受；
	// 值 "inherit"/空 = 跟随 DefaultEffort，具体档位 = 该模型专属默认档。
	// 由面板「模型能力」页行内编辑写入 thinking.model_defaults，保存即热生效。
	ModelDefaults map[string]string
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

// DefaultEffortFor 返回某模型实际使用的网关默认档：先查每模型覆盖
// （"realm:model" 优先，裸模型名兜底），未覆盖或标记 inherit 时回落到全局
// DefaultEffort。返回值尚未按模型声明档位解析，调用方仍需 ResolveGatewayEffort。
func (p EffortPolicy) DefaultEffortFor(realm, model string) string {
	p = p.normalized()
	if v := p.ModelDefault(realm, model); v != "" {
		return v
	}
	return p.DefaultEffort
}

// ModelDefault 返回该模型的显式覆盖档；未覆盖或标记 inherit 时返回 ""。
// 键按 "realm:model"（realm 空等价 cn）优先、裸模型名兜底。
func (p EffortPolicy) ModelDefault(realm, model string) string {
	model = strings.TrimSpace(model)
	if model == "" || len(p.ModelDefaults) == 0 {
		return ""
	}
	key := realmKey(realm) + ":" + model
	if v, ok := p.ModelDefaults[key]; ok {
		return normalizeModelDefault(v)
	}
	return normalizeModelDefault(p.ModelDefaults[model])
}

// normalizeModelDefault 归一化覆盖值：inherit/空 表示未覆盖。
func normalizeModelDefault(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" || s == "inherit" {
		return ""
	}
	return s
}

// copyModelDefaults 深拷贝每模型覆盖表（快照与配置解耦，避免共享 map 被并发改写）。
func copyModelDefaults(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

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

// honorClose 尊重客户端关闭思考：清掉 off/none 这类非法档位值；deepseek 额外写
// thinking.type=disabled（该域唯一能落地的关闭形态），避免 injectThinking 又把默认档补回来。
func honorClose(obj map[string]any, model string) {
	delete(obj, "reasoning_effort")
	delete(obj, "reasoningEffort")
	if !isDeepSeekModel(model) {
		return
	}
	if t, ok := obj["thinking"].(map[string]any); ok {
		t["type"] = "disabled"
		return
	}
	obj["thinking"] = map[string]any{"type": "disabled"}
}

// applyEffortPolicy 按网关策略写入生效档位。必须在 injectThinking /
// normalizeReasoningEffort 之前调用：前者只补 deepseek 的默认档、后者只管降级，
// 都不负责「客户端缺档/优先级/覆盖关闭」这三类决定。
//
// 只处理模型确实声明了档位（efforts[model] 非空）的请求——没有声明就绝不猜档位，
// 也没有可写入的合法值。
func applyEffortPolicy(obj map[string]any, model string, efforts map[string][]string, realm string, policy *EffortPolicy) {
	if policy == nil || !policy.Enabled || len(efforts) == 0 {
		return
	}
	supported := efforts[model]
	if len(supported) == 0 {
		return
	}
	p := policy.normalized()
	target, ok := ResolveGatewayEffort(p.DefaultEffortFor(realm, model), supported)
	if !ok {
		return
	}

	key, current, present := effortField(obj)
	off := present && isEffortOff(current)
	disabled := thinkingDisabled(obj)

	// 客户端表达关闭：客户端优先、或网关优先但只覆盖档位（level）时尊重它。
	// 只有「网关优先 + all」才连开关一起翻开。
	if off || disabled {
		if p.ClientPriority || p.OverrideScope != OverrideScopeAll {
			honorClose(obj, model)
			return
		}
		// all：只翻转请求里已有的 thinking 对象，不凭空新增（非 deepseek 上游可能不认）；
		// deepseek 缺 thinking 时由 injectThinking 负责补 enabled。
		if t, ok := obj["thinking"].(map[string]any); ok {
			t["type"] = "enabled"
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
