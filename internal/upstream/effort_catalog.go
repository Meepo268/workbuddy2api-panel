// effort_catalog.go 推理档位（reasoning effort）产品级静态兜底表。
//
// 数据来源三级（吸收参考仓库 reconcileWithFallback/buddy-adapter.ts:499-523 语义）：
//   - 远端 FetchModels / global 探测已解析的 supportedEfforts/defaultEffort 桶（权威，优先）；
//   - 本文件按 realm 分开的产品级静态兜底表（远端缺失时补齐）；
//   - 两者皆无 → 不在 /v1/models 输出 effort 字段（omitted，不是空数组）。
//
// 档位值照抄参考仓库 dsh-codearts：CN/CodeBuddy 面取 src/product.ts CODEBUDDY_FALLBACK_MODELS
// ∪ src/buddy-adapter.ts:126-137 REASONING_EFFORTS；global/WorkBuddy 面取 src/product.ts
// WORKBUDDY_FALLBACK_MODELS。两个 realm 对同一模型给出不同档位（如 deepseek-v4.1-flash：
// CN 三档 ['low','high','max']、global 仅 ['high']），故**按 realm 分表**，绝不混用。
//
// 只列**可枚举**档位的模型；仅有固定默认档（glm-5.1/kimi-* 的 medium）同样入表，
// 但其档位是「可枚举单档」而非「无选择器」，照抄参考仓库如实暴露。
package upstream

import "strings"

// effortCap 一个模型的档位能力（对齐产品兜底表条目 reasoningEfforts + defaultReasoningEffort）。
type effortCap struct {
	efforts       []string
	defaultEffort string
}

// cnEffortFallback CN / CodeBuddy 面静态兜底表。
// 三档模型 defaultEffort 均为 high（product.ts CODEBUDDY_FALLBACK_MODELS 逐条 defaultReasoningEffort）。
var cnEffortFallback = map[string]effortCap{
	"deepseek-v4-flash":   {efforts: []string{"low", "high", "max"}},
	"deepseek-v4.1-flash": {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"deepseek-v4-pro":     {efforts: []string{"low", "high", "xhigh"}, defaultEffort: "high"},
	"hy4-preview":         {efforts: []string{"high"}, defaultEffort: "high"},
	"hy4-preview-x":       {efforts: []string{"high"}},
	"hy3":                 {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"hy3-x":               {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"glm-5.3":             {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.3-flash":       {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.2":             {efforts: []string{"high", "xhigh"}, defaultEffort: "high"},
	"glm-5.1":             {efforts: []string{"medium"}},
	"glm-5v-turbo":        {efforts: []string{"medium"}},
	"kimi-k3-1":           {efforts: []string{"medium"}},
	"kimi-k2.7":           {efforts: []string{"medium"}},
	"kimi-k2.6":           {efforts: []string{"medium"}},
	"minimax-m3":          {efforts: []string{"medium"}},
}

// globalEffortFallback global / WorkBuddy 国际版面静态兜底表。
// 注意 deepseek-v4.1-flash 在国际版**只有 ['high']**（product.ts:190 实测 IDE 缓存），
// 与 CN 面的三档刻意不同——往 WorkBuddy 上游发 low/max 是非法参数 400。
var globalEffortFallback = map[string]effortCap{
	"fast-model":          {efforts: []string{"medium"}},
	"balanced-model":      {efforts: []string{"medium"}},
	"primary-model":       {efforts: []string{"high"}},
	"hy4-preview-f":       {efforts: []string{"high"}, defaultEffort: "high"},
	"hy3":                 {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"deepseek-v4.1-flash": {efforts: []string{"high"}},
	"gpt-6-astra":         {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-sol":         {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-terra":       {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-luna":        {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.5":             {efforts: []string{"low", "medium", "high", "xhigh"}, defaultEffort: "high"},
	"gpt-5.4":             {efforts: []string{"low", "medium", "high", "xhigh"}, defaultEffort: "high"},
	"gpt-5.3-codex":       {efforts: []string{"medium"}},
	"gemini-3.5-flash":    {efforts: []string{"medium"}},
	"glm-5.3":             {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.2":             {efforts: []string{"high", "xhigh"}, defaultEffort: "high"},
	"kimi-k3":             {efforts: []string{"medium"}},
	"kimi-k2.6":           {efforts: []string{"medium"}},
}

// staticEffortCap 按 realm 取静态兜底条目；未命中返回 zero effortCap（efforts=nil）。
// realm 经 realmKey 归一化（空 → "cn"），与 efforts 缓存桶同口径。
func staticEffortCap(realm, model string) effortCap {
	table := cnEffortFallback
	if realmKey(realm) == "global" {
		table = globalEffortFallback
	}
	return table[model]
}

// EffortListing 计算模型在 /v1/models 应暴露的 effort 能力（三级查找 + 默认档防御）。
//
// remoteEfforts/remoteDefault 为远端（FetchModels / global 探测）已解析值；
// remoteEfforts 非空时以其为权威（不回落到静态表），否则落到产品级静态兜底表；
// 两者皆无 → efforts 返回 nil（调用方省略字段，不输出空数组）。
//
// defaultEffort 仅在「efforts 非空且 default 命中 efforts」时才返回
// （对齐参考仓库 resolveModel 的 `defaultEffort ∈ efforts` 防御：不宣称不支持的默认档）。
// remoteDefault 空串不回落到静态默认档——默认档随档位表同源：remote 有档位就用 remote 默认档，
// 静态兜底档位就用静态默认档，避免跨源拼接出「档位是静态、默认档是 remote」的矛盾组合。
func EffortListing(realm, model string, remoteEfforts []string, remoteDefault string) (efforts []string, defaultEffort string) {
	var src effortCap
	switch {
	case len(remoteEfforts) > 0:
		src = effortCap{efforts: remoteEfforts, defaultEffort: remoteDefault}
	default:
		src = staticEffortCap(realm, model)
	}
	if len(src.efforts) == 0 {
		return nil, ""
	}
	efforts = append([]string(nil), src.efforts...)
	if src.defaultEffort != "" && containsEffort(efforts, src.defaultEffort) {
		defaultEffort = src.defaultEffort
	}
	return efforts, defaultEffort
}

// containsEffort 档位成员判定（精确匹配，对齐参考仓库 `efforts.includes(defaultEffort)`）。
func containsEffort(efforts []string, want string) bool {
	for _, e := range efforts {
		if e == want {
			return true
		}
	}
	return false
}

// effortLevelRank 档位标准名判定：大小写/空白归一后必须命中 effortRank。
func effortLevelRank(level string) (string, int, bool) {
	lv := strings.ToLower(strings.TrimSpace(level))
	idx, ok := effortRank[lv]
	return lv, idx, ok
}

// IsEffortLevel 判断字符串是否是合法档位名（off/minimal/low/medium/high/xhigh/max）。
func IsEffortLevel(level string) bool {
	_, _, ok := effortLevelRank(level)
	return ok
}

// highestEffort 取声明档位里的最高一档（按 effortRank 判据，不依赖列表顺序）。
// 未知档全部忽略；一个都不认识时返回 false。
func highestEffort(efforts []string) (string, bool) {
	best := ""
	bestIdx := -1
	for _, e := range efforts {
		lv, idx, ok := effortLevelRank(e)
		if !ok {
			continue
		}
		if idx > bestIdx {
			best, bestIdx = lv, idx
		}
	}
	return best, bestIdx >= 0
}

// ResolveGatewayEffort 计算网关策略的生效档位。
//
// configured 为空或 "max" 表示「该模型声明档位里的最高档」；具体档位必须是模型
// 支持的档位，否则同样退化为最高档——绝不向出站请求写入模型不支持的档位。
// 模型没有声明档位时返回 false（调用方保持原样）。
func ResolveGatewayEffort(configured string, supported []string) (string, bool) {
	if len(supported) == 0 {
		return "", false
	}
	want := strings.ToLower(strings.TrimSpace(configured))
	if want == "" || want == "max" {
		return highestEffort(supported)
	}
	for _, e := range supported {
		if strings.EqualFold(strings.TrimSpace(e), want) {
			return strings.TrimSpace(e), true
		}
	}
	return highestEffort(supported)
}

// requestEffortMap 组装请求侧（降级/补档）用的档位能力表：本 realm 静态兜底表为基，
// 远端探测桶按模型覆盖（权威优先）。
//
// 与展示侧 EffortListing 同源规则：远端给了某模型档位时，默认档只认远端同批下发的
// 值；远端没给默认档就删掉静态默认档，避免「档位来自远端、默认档来自静态表」的跨源
// 拼接（见 EffortListing 注释）。远端没有档位的模型继续沿用静态表。
func requestEffortMap(realm string, remoteEfforts map[string][]string, remoteDefaults map[string]string) (map[string][]string, map[string]string) {
	table := cnEffortFallback
	if realmKey(realm) == "global" {
		table = globalEffortFallback
	}
	efforts := make(map[string][]string, len(table)+len(remoteEfforts))
	defs := make(map[string]string, len(table)+len(remoteDefaults))
	for id, cap := range table {
		efforts[id] = append([]string(nil), cap.efforts...)
		if cap.defaultEffort != "" {
			defs[id] = cap.defaultEffort
		}
	}
	for id, v := range remoteEfforts {
		if len(v) == 0 {
			continue
		}
		efforts[id] = v
		if d := strings.TrimSpace(remoteDefaults[id]); d != "" {
			defs[id] = d
		} else {
			delete(defs, id)
		}
	}
	return efforts, defs
}

// globalEffortMap global 域兼容封装：见 requestEffortMap。
//
// prepareBody 对 global 请求调此函数（而非直接用远端桶），因为 global 上游可能不下发
// supportedEfforts——此时也必须按产品静态表降级（issue #84：deepseek-v4.1-flash 国际版
// 只认 high，客户端传 low/max 必降级到 high，否则上游 400 毁掉请求）。
func globalEffortMap(remoteEfforts map[string][]string, remoteDefaults map[string]string) (map[string][]string, map[string]string) {
	return requestEffortMap("global", remoteEfforts, remoteDefaults)
}
