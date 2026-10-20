// models_thinking_test.go 「模型能力」页行内档位编辑：只动 thinking.model_defaults，
// 其余配置原样回写；非法输入不落盘。
package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newThinkingTestPanel(saved *string) *Panel {
	return &Panel{cfg: Config{
		LoadConfig: func() (any, error) {
			return map[string]any{
				"listen":   ":7863",
				"thinking": map[string]any{"enabled": true, "default_effort": "max"},
			}, nil
		},
		SaveConfig: func(raw []byte) ([]string, error) {
			*saved = string(raw)
			return nil, nil
		},
	}}
}

func postModelThinking(t *testing.T, p *Panel, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/models/thinking", strings.NewReader(body))
	w := httptest.NewRecorder()
	p.modelThinking(w, req)
	return w
}

func TestModelThinkingSetAndClear(t *testing.T) {
	var saved string
	p := newThinkingTestPanel(&saved)

	w := postModelThinking(t, p, `{"id":"cn:demo","effort":"HIGH"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(saved), &doc); err != nil {
		t.Fatalf("落盘 JSON 非法: %v (%s)", err, saved)
	}
	thinking, _ := doc["thinking"].(map[string]any)
	defaults, _ := thinking["model_defaults"].(map[string]any)
	if got, _ := defaults["cn:demo"].(string); got != "high" {
		t.Errorf("model_defaults[cn:demo]=%q want high (%s)", got, saved)
	}
	if thinking["default_effort"] != "max" || thinking["enabled"] != true {
		t.Errorf("其余 thinking 字段应保留，got %v", thinking)
	}

	// inherit → 删除该键；清空后整个 model_defaults 键消失。
	w = postModelThinking(t, p, `{"id":"cn:demo","effort":"inherit"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	doc = map[string]any{}
	_ = json.Unmarshal([]byte(saved), &doc)
	thinking, _ = doc["thinking"].(map[string]any)
	if _, has := thinking["model_defaults"]; has {
		t.Errorf("清空后应删除 model_defaults 键，got %s", saved)
	}
}

func TestModelThinkingRejectsBadInput(t *testing.T) {
	var saved string
	p := newThinkingTestPanel(&saved)
	for _, body := range []string{
		`{"id":"","effort":"high"}`,         // 空 id
		`{"id":"cn:","effort":"high"}`,      // 缺模型
		`{"id":"cn:demo","effort":"turbo"}`, // 非法档位
		`{"id":`,                            // 坏 JSON
	} {
		w := postModelThinking(t, p, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%s status=%d want 400", body, w.Code)
		}
	}
	if saved != "" {
		t.Errorf("非法请求不应调用 SaveConfig, saved=%s", saved)
	}
}

func TestModelThinkingUnavailableWithoutConfigAPI(t *testing.T) {
	p := &Panel{}
	w := postModelThinking(t, p, `{"id":"cn:demo","effort":"high"}`)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("status=%d want 501", w.Code)
	}
}
