package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/scheduler"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestThinkingDefaults(t *testing.T) {
	c := Default()
	if !c.Thinking.Enabled || c.Thinking.DefaultEffort != "max" || !c.Thinking.ClientPriority || c.Thinking.OverrideScope != "level" {
		t.Fatalf("thinking 默认值=%+v，want enabled/max/client_priority/level", c.Thinking)
	}
}

func TestThinkingParsedAndNormalized(t *testing.T) {
	c, err := ParseConfig([]byte(`{"thinking":{"enabled":false,"default_effort":" HIGH ","client_priority":false,"override_scope":" ALL "}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Thinking.Enabled || c.Thinking.ClientPriority {
		t.Errorf("布尔项解析=%+v", c.Thinking)
	}
	if c.Thinking.DefaultEffort != "high" {
		t.Errorf("default_effort=%q want high（小写去空格）", c.Thinking.DefaultEffort)
	}
	if c.Thinking.OverrideScope != "all" {
		t.Errorf("override_scope=%q want all", c.Thinking.OverrideScope)
	}
}

func TestThinkingInvalidEffortRejected(t *testing.T) {
	_, err := ParseConfig([]byte(`{"thinking":{"default_effort":"turbo"}}`))
	if err == nil || !strings.Contains(err.Error(), "thinking.default_effort") {
		t.Fatalf("err=%v want thinking.default_effort 报错", err)
	}
}

func TestThinkingInvalidScopeRejected(t *testing.T) {
	_, err := ParseConfig([]byte(`{"thinking":{"override_scope":"everything"}}`))
	if err == nil || !strings.Contains(err.Error(), "thinking.override_scope") {
		t.Fatalf("err=%v want thinking.override_scope 报错", err)
	}
}

func TestThinkingEmptyScopeFallsBackToLevel(t *testing.T) {
	c, err := ParseConfig([]byte(`{"thinking":{"override_scope":"  "}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Thinking.OverrideScope != "level" {
		t.Errorf("空 scope=%q want level", c.Thinking.OverrideScope)
	}
}

func TestThinkingEnvOverride(t *testing.T) {
	t.Setenv("WB2A_THINKING_ENABLED", "false")
	t.Setenv("WB2A_THINKING_DEFAULT_EFFORT", "High")
	t.Setenv("WB2A_THINKING_CLIENT_PRIORITY", "false")
	t.Setenv("WB2A_THINKING_OVERRIDE_SCOPE", "all")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Thinking.Enabled || c.Thinking.ClientPriority || c.Thinking.DefaultEffort != "high" || c.Thinking.OverrideScope != "all" {
		t.Fatalf("env 覆盖后=%+v", c.Thinking)
	}
	managed := c.EnvironmentManagedFields()
	for _, want := range []string{"thinking.enabled", "thinking.default_effort", "thinking.client_priority", "thinking.override_scope"} {
		found := false
		for _, f := range managed {
			found = found || f == want
		}
		if !found {
			t.Errorf("EnvironmentManagedFields 缺 %s: %v", want, managed)
		}
	}
}

// TestThinkingHotReload 保存 thinking 段应热生效且不产生重启项。
func TestThinkingHotReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	live := livecfg.New(livecfg.Snapshot{APIKey: cfg.APIKey})
	p := pool.New("")
	defer p.Close()
	up := upstream.New()
	sch := scheduler.New(scheduler.Config{Pool: p, Upstream: up})
	fields, err := saveConfig([]byte(`{"thinking":{"enabled":false,"default_effort":"xhigh","client_priority":false,"override_scope":"all"}}`),
		path, live, p, up, sch, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 0 {
		t.Fatalf("thinking 变更不应要求重启，got %v", fields)
	}
	snap := up.EffortPolicySnapshot()
	if snap == nil || snap.Enabled || snap.DefaultEffort != "xhigh" || snap.ClientPriority || snap.OverrideScope != "all" {
		t.Fatalf("热应用快照=%+v", snap)
	}
}
