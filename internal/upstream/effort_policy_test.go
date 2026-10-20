package upstream

import (
	"encoding/json"
	"testing"
)

func TestHighestEffortOrderIndependent(t *testing.T) {
	cases := []struct {
		in   []string
		want string
		ok   bool
	}{
		{[]string{"high", "low", "max", "medium"}, "max", true},
		{[]string{"max", "high"}, "max", true},
		{[]string{"low"}, "low", true},
		{[]string{"off"}, "off", true},
		{[]string{"ultra", "turbo"}, "", false},
		{nil, "", false},
	}
	for _, c := range cases {
		got, ok := highestEffort(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("highestEffort(%v)=(%q,%v) want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestResolveGatewayEffort(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		supported  []string
		want       string
		ok         bool
	}{
		{"空配置取最高档", "", []string{"low", "high", "max"}, "max", true},
		{"max 取声明最高档", "max", []string{"low", "high"}, "high", true},
		{"大小写/空白归一", "  MAX ", []string{"low", "xhigh"}, "xhigh", true},
		{"配置档位受支持则原样", "high", []string{"low", "high", "max"}, "high", true},
		{"配置档位不受支持退化为最高档", "medium", []string{"low", "high", "max"}, "max", true},
		{"未知档位退化为最高档", "ultra", []string{"low", "high"}, "high", true},
		{"模型无声明档位 → false", "max", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ResolveGatewayEffort(c.configured, c.supported)
			if got != c.want || ok != c.ok {
				t.Errorf("ResolveGatewayEffort(%q,%v)=(%q,%v) want (%q,%v)", c.configured, c.supported, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestIsEffortLevel(t *testing.T) {
	for _, lv := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max", " MAX "} {
		if !IsEffortLevel(lv) {
			t.Errorf("IsEffortLevel(%q)=false want true", lv)
		}
	}
	for _, lv := range []string{"", "ultra", "none", "diabled"} {
		if IsEffortLevel(lv) {
			t.Errorf("IsEffortLevel(%q)=true want false", lv)
		}
	}
}

func TestRequestEffortMapRealmAndOverride(t *testing.T) {
	t.Run("CN 静态表为基", func(t *testing.T) {
		efforts, defs := requestEffortMap("cn", nil, nil)
		if got := efforts["glm-5.3"]; len(got) == 0 || got[len(got)-1] != "max" {
			t.Errorf("glm-5.3 efforts=%v want 命中 CN 静态表", got)
		}
		if defs["glm-5.3"] != "high" {
			t.Errorf("glm-5.3 default=%q want high", defs["glm-5.3"])
		}
		if _, ok := efforts["gpt-5.5"]; ok {
			t.Errorf("gpt-5.5 只属于 global 静态表，不应出现在 CN")
		}
	})
	t.Run("global 静态表为基", func(t *testing.T) {
		efforts, _ := requestEffortMap("global", nil, nil)
		if _, ok := efforts["gpt-5.5"]; !ok {
			t.Errorf("global 静态表应含 gpt-5.5")
		}
	})
	t.Run("远端覆盖静态且默认档同源", func(t *testing.T) {
		efforts, defs := requestEffortMap("cn",
			map[string][]string{"glm-5.3": {"low", "xhigh"}, "new-model": {"high"}},
			map[string]string{"new-model": "high"})
		if got := efforts["glm-5.3"]; len(got) != 2 || got[0] != "low" || got[1] != "xhigh" {
			t.Errorf("glm-5.3 efforts=%v want 远端值", got)
		}
		// 远端档位里没有 high：静态默认档必须被删掉，不能跨源拼接。
		if d, ok := defs["glm-5.3"]; ok {
			t.Errorf("glm-5.3 default=%q 应删除（远端未下发默认档）", d)
		}
		if defs["new-model"] != "high" {
			t.Errorf("new-model default=%q want high", defs["new-model"])
		}
	})
	t.Run("远端空档位不覆盖静态", func(t *testing.T) {
		efforts, defs := requestEffortMap("cn", map[string][]string{"glm-5.3": {}}, nil)
		if len(efforts["glm-5.3"]) == 0 {
			t.Errorf("远端空档位不应把静态档位清空")
		}
		if defs["glm-5.3"] != "high" {
			t.Errorf("glm-5.3 default=%q want high（静态）", defs["glm-5.3"])
		}
	})
}

// effortOf 读取输出体里的生效档位（snake 优先，其次 camel）。
func effortOf(t *testing.T, body []byte) (key, val string, present bool) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	return effortField(m)
}

func TestEffortPolicyMatrix(t *testing.T) {
	efforts := map[string][]string{
		"demo":                {"low", "high", "max"},
		"deepseek-v4.1-flash": {"low", "high", "max"},
		"narrow":              {"low", "xhigh"},
	}
	on := func(clientPriority bool, scope string) *EffortPolicy {
		return &EffortPolicy{Enabled: true, DefaultEffort: "max", ClientPriority: clientPriority, OverrideScope: scope}
	}
	cases := []struct {
		name       string
		body       string
		policy     *EffortPolicy
		wantKey    string
		wantVal    string
		wantNoEff  bool
		wantThink  string // 期望 thinking.type；空表示不断言
		assertOnly bool
	}{
		{
			name: "关闭策略不介入", body: `{"model":"demo","messages":[]}`,
			policy: &EffortPolicy{Enabled: false}, wantNoEff: true,
		},
		{
			name: "客户端缺档 + 客户端优先 → 补最高档", body: `{"model":"demo","messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantKey: "reasoning_effort", wantVal: "max",
		},
		{
			name: "客户端缺档 + 网关优先 → 补最高档", body: `{"model":"demo","messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantKey: "reasoning_effort", wantVal: "max",
		},
		{
			name: "显式档位 + 客户端优先 → 保留", body: `{"model":"demo","reasoning_effort":"low","messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantKey: "reasoning_effort", wantVal: "low",
		},
		{
			name: "显式档位 + 网关优先 → 覆盖", body: `{"model":"demo","reasoning_effort":"low","messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantKey: "reasoning_effort", wantVal: "max",
		},
		{
			name: "camelCase 显式档位 + 网关优先 → 改原字段不新增 snake", body: `{"model":"demo","reasoningEffort":"low","messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantKey: "reasoningEffort", wantVal: "max",
		},
		{
			name: "off + 客户端优先 → 尊重关闭，不补档", body: `{"model":"demo","reasoning_effort":"off","messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantNoEff: true,
		},
		{
			// off 是档位形态的关闭意图：scope=level 只覆盖档位、不覆盖开关，
			// 因此与 thinking.type=disabled 同样被尊重。
			name: "off + 网关优先 scope=level → 尊重关闭，不补档", body: `{"model":"demo","reasoning_effort":"off","messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantNoEff: true,
		},
		{
			name: "off + 网关优先 scope=all → 翻开并补档", body: `{"model":"demo","reasoning_effort":"off","messages":[]}`,
			policy: on(false, OverrideScopeAll), wantKey: "reasoning_effort", wantVal: "max",
		},
		{
			name: "deepseek off + 客户端优先 → thinking 落 disabled、无档位", body: `{"model":"deepseek-v4.1-flash","reasoning_effort":"off","messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantNoEff: true, wantThink: "disabled",
		},
		{
			name: "deepseek off + 网关优先 scope=all → 翻开并补档", body: `{"model":"deepseek-v4.1-flash","reasoning_effort":"off","messages":[]}`,
			policy: on(false, OverrideScopeAll), wantKey: "reasoning_effort", wantVal: "max", wantThink: "enabled",
		},
		{
			name: "thinking disabled + 客户端优先 → 尊重关闭", body: `{"model":"demo","thinking":{"type":"disabled"},"messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantNoEff: true, wantThink: "disabled",
		},
		{
			name: "thinking disabled + 网关优先 scope=level → 只覆盖档位，不翻开关", body: `{"model":"demo","thinking":{"type":"disabled"},"messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantNoEff: true, wantThink: "disabled",
		},
		{
			name: "thinking disabled + 网关优先 scope=all → 翻开关并补档", body: `{"model":"demo","thinking":{"type":"disabled"},"messages":[]}`,
			policy: on(false, OverrideScopeAll), wantKey: "reasoning_effort", wantVal: "max", wantThink: "enabled",
		},
		{
			name: "配置档位不支持 → 用声明最高档", body: `{"model":"narrow","messages":[]}`,
			policy:  &EffortPolicy{Enabled: true, DefaultEffort: "high", ClientPriority: false, OverrideScope: OverrideScopeLevel},
			wantKey: "reasoning_effort", wantVal: "xhigh",
		},
		{
			name: "模型无声明档位 → 不写档位", body: `{"model":"other","messages":[]}`,
			policy: on(true, OverrideScopeLevel), wantNoEff: true,
		},
		{
			name: "deepseek 缺档：策略档位先写入，thinking 注入不与默认档打架", body: `{"model":"deepseek-v4.1-flash","messages":[]}`,
			policy: on(false, OverrideScopeLevel), wantKey: "reasoning_effort", wantVal: "max", wantThink: "enabled",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := PrepareBodyOptWithEffortPolicy([]byte(c.body), false, efforts, nil, nil, "", c.policy)
			key, val, present := effortOf(t, out)
			if c.wantNoEff {
				if present {
					t.Errorf("不应写档位，得到 %s=%q (body=%s)", key, val, out)
				}
			} else {
				if !present || key != c.wantKey || val != c.wantVal {
					t.Errorf("档位=(%q,%q,%v) want (%q,%q,true) (body=%s)", key, val, present, c.wantKey, c.wantVal, out)
				}
			}
			if c.wantThink != "" {
				var m map[string]any
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				think, _ := m["thinking"].(map[string]any)
				if got, _ := think["type"].(string); got != c.wantThink {
					t.Errorf("thinking.type=%q want %q (body=%s)", got, c.wantThink, out)
				}
			}
		})
	}
}

func TestEffortPolicyNilKeepsLegacyPipeline(t *testing.T) {
	body := []byte(`{"model":"demo","reasoning_effort":"low","messages":[]}`)
	efforts := map[string][]string{"demo": {"low", "high"}}
	legacy := PrepareBodyOptWithEffortsAndDefault(body, false, efforts, nil)
	withNil, _ := PrepareBodyOptWithEffortPolicy(body, false, efforts, nil, nil, "", nil)
	if string(legacy) != string(withNil) {
		t.Errorf("policy=nil 应与旧管线完全一致\nlegacy=%s\nnew=%s", legacy, withNil)
	}
}

func TestClientSetEffortPolicy(t *testing.T) {
	c := New()
	if c.EffortPolicySnapshot() != nil {
		t.Fatalf("默认应为 nil")
	}
	c.SetEffortPolicy(&EffortPolicy{Enabled: true, ClientPriority: true, OverrideScope: "ALL", DefaultEffort: " MAX "})
	got := c.EffortPolicySnapshot()
	if got == nil || !got.Enabled || !got.ClientPriority || got.OverrideScope != OverrideScopeAll || got.DefaultEffort != "max" {
		t.Fatalf("归一化后快照=%+v", got)
	}
	// 快照不随外部修改而变（Store 的是副本）。
	got.DefaultEffort = "low"
	if again := c.EffortPolicySnapshot(); again == nil || again.DefaultEffort != "max" {
		t.Fatalf("快照被外部修改污染: %+v", again)
	}
	c.SetEffortPolicy(nil)
	if c.EffortPolicySnapshot() != nil {
		t.Fatalf("SetEffortPolicy(nil) 应清空")
	}
}

// TestEffortPolicyModelDefaults 每模型覆盖解析：realm 键优先、裸名兜底、
// inherit 回落全局；快照深拷贝，外部改 map 不影响已存策略。
func TestEffortPolicyModelDefaults(t *testing.T) {
	p := &EffortPolicy{
		Enabled: true, DefaultEffort: "max",
		ModelDefaults: map[string]string{
			"cn:glm-5.3": "high",
			"kimi-k3":    "low",
			"global:x":   "inherit",
		},
	}
	if got := p.DefaultEffortFor("cn", "glm-5.3"); got != "high" {
		t.Errorf("cn:glm-5.3=%q want high", got)
	}
	if got := p.DefaultEffortFor("global", "glm-5.3"); got != "max" {
		t.Errorf("global 无覆盖应回落 max, got %q", got)
	}
	if got := p.DefaultEffortFor("cn", "kimi-k3"); got != "low" {
		t.Errorf("裸名兜底=%q want low", got)
	}
	if got := p.DefaultEffortFor("global", "x"); got != "max" {
		t.Errorf("inherit 应回落 max, got %q", got)
	}
	if got := p.DefaultEffortFor("", "glm-5.3"); got != "high" {
		t.Errorf("空 realm 等价 cn, got %q", got)
	}
	if got := p.ModelDefault("cn", "glm-5.3"); got != "high" {
		t.Errorf("ModelDefault=%q want high", got)
	}
	if got := p.ModelDefault("cn", "none"); got != "" {
		t.Errorf("未覆盖 ModelDefault=%q want 空", got)
	}

	c := New()
	c.SetEffortPolicy(p)
	p.ModelDefaults["cn:glm-5.3"] = "low" // 改原 map 不应污染快照
	if got := c.EffortPolicySnapshot().DefaultEffortFor("cn", "glm-5.3"); got != "high" {
		t.Errorf("快照被外部 map 污染: %q", got)
	}
}

// TestPrepareBodyOptPerModelRealm 出站补档读 realm 覆盖：同一模型两域可不同，
// 目标是模型声明档位，补出的值必在 supported 内。
func TestPrepareBodyOptPerModelRealm(t *testing.T) {
	p := &EffortPolicy{
		Enabled: true, DefaultEffort: "max", ClientPriority: true, OverrideScope: "level",
		ModelDefaults: map[string]string{"cn:demo": "low"},
	}
	efforts := map[string][]string{"demo": {"low", "medium", "high"}}
	body := []byte(`{"model":"demo","messages":[]}`)
	out, _ := PrepareBodyOptWithEffortPolicy(body, false, efforts, nil, nil, "cn", p)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["reasoning_effort"] != "low" {
		t.Errorf("cn 覆盖档=%v want low (out=%s)", got["reasoning_effort"], out)
	}
	// 同模型 global 域没有覆盖 → 全局 max → 声明最高档 high。
	out, _ = PrepareBodyOptWithEffortPolicy(body, false, efforts, nil, nil, "global", p)
	got = map[string]any{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["reasoning_effort"] != "high" {
		t.Errorf("global 默认档=%v want high (out=%s)", got["reasoning_effort"], out)
	}
}
