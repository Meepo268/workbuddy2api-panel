// models_hidden_test.go 「模型能力」页行内「下游可见」开关：只动 models.hidden，
// 与配置页黑名单共用同一份名单；裸名条目在关闭单域时要拆开，别误伤另一域。
package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
)

func TestSetModelHidden(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		id      string
		hidden  bool
		want    []string
	}{
		{"hide-empty", nil, "cn:a", true, []string{"cn:a"}},
		{"hide-keeps-existing", []string{"global:b"}, "cn:a", true, []string{"global:b", "cn:a"}},
		{"hide-idempotent", []string{"cn:a"}, "cn:a", true, []string{"cn:a"}},
		{"unhide-exact", []string{"cn:a", "global:b"}, "cn:a", false, []string{"global:b"}},
		// 裸名同时盖两域：只关 cn 时另一域改写成精确条目，否则 global 也被放出来。
		{"unhide-splits-bare", []string{"a"}, "cn:a", false, []string{"global:a"}},
		{"unhide-splits-bare-other-realm", []string{"a", "cn:a"}, "global:a", false, []string{"cn:a"}},
		{"unhide-unknown-keeps", []string{"cn:a"}, "cn:b", false, []string{"cn:a"}},
		{"invalid-id-noop", []string{"cn:a"}, "a", true, []string{"cn:a"}},
		{"invalid-realm-noop", []string{"cn:a"}, "us:a", true, []string{"cn:a"}},
		{"empty-bare-noop", []string{"cn:a"}, "cn:", true, []string{"cn:a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := setModelHidden(c.entries, c.id, c.hidden)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("setModelHidden(%v,%q,%v)=%v want %v", c.entries, c.id, c.hidden, got, c.want)
			}
		})
	}
}

func TestApplyHiddenFlagsAndModelHidden(t *testing.T) {
	live := livecfg.New(livecfg.Snapshot{HiddenModels: map[string]struct{}{
		"cn:a":     {},
		"global:b": {},
	}})
	p := &Panel{cfg: Config{Live: live}}
	entries := []map[string]any{
		{"id": "cn:a"},
		{"id": "cn:b"},
		{"id": "global:b"},
	}
	p.applyHiddenFlags(entries)
	if entries[0]["hidden"] != true {
		t.Errorf("cn:a should be flagged hidden: %v", entries[0])
	}
	if _, has := entries[1]["hidden"]; has {
		t.Errorf("cn:b should not be flagged: %v", entries[1])
	}
	if entries[2]["hidden"] != true {
		t.Errorf("global:b should be flagged hidden: %v", entries[2])
	}
	if !p.modelHidden("cn", "a") || p.modelHidden("global", "a") {
		t.Errorf("modelHidden realm isolation broken")
	}
	if p.modelHidden("cn", "b") {
		t.Errorf("cn:b should be visible")
	}
}

func TestApplyHiddenFlagsNoLive(t *testing.T) {
	p := &Panel{}
	entries := []map[string]any{{"id": "cn:a"}}
	p.applyHiddenFlags(entries) // 不应 panic
	if _, has := entries[0]["hidden"]; has {
		t.Errorf("no live holder: should not flag")
	}
}

func newHiddenTestPanel(saved *string) *Panel {
	return &Panel{cfg: Config{
		LoadConfig: func() (any, error) {
			return map[string]any{
				"listen": ":7863",
				"models": map[string]any{"hidden": []any{"cn:old", "shared"}},
			}, nil
		},
		SaveConfig: func(raw []byte) ([]string, error) {
			*saved = string(raw)
			return []string{}, nil
		},
	}}
}

func postModelHidden(t *testing.T, p *Panel, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/models/hidden", strings.NewReader(body))
	w := httptest.NewRecorder()
	p.modelHiddenToggle(w, req)
	return w
}

func TestModelHiddenToggleWritesList(t *testing.T) {
	var saved string
	p := newHiddenTestPanel(&saved)

	w := postModelHidden(t, p, `{"id":"cn:demo","hidden":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(saved), &doc); err != nil {
		t.Fatalf("落盘 JSON 非法: %v (%s)", err, saved)
	}
	models, _ := doc["models"].(map[string]any)
	list, _ := models["hidden"].([]any)
	got := make([]string, 0, len(list))
	for _, v := range list {
		got = append(got, v.(string))
	}
	if strings.Join(got, ",") != "cn:old,shared,cn:demo" {
		t.Errorf("hidden=%v want [cn:old shared cn:demo] (%s)", got, saved)
	}

	// 关闭一个裸名覆盖的域：另一域要保留为精确条目。
	saved = ""
	w = postModelHidden(t, p, `{"id":"cn:shared","hidden":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	doc = map[string]any{}
	_ = json.Unmarshal([]byte(saved), &doc)
	models, _ = doc["models"].(map[string]any)
	list, _ = models["hidden"].([]any)
	got = got[:0]
	for _, v := range list {
		got = append(got, v.(string))
	}
	if strings.Join(got, ",") != "cn:old,global:shared" {
		t.Errorf("hidden=%v want [cn:old global:shared] (%s)", got, saved)
	}
}

func TestModelHiddenToggleClearWritesNull(t *testing.T) {
	var saved string
	p := &Panel{cfg: Config{
		LoadConfig: func() (any, error) {
			return map[string]any{
				"listen": ":7863",
				"models": map[string]any{"hidden": []any{"cn:demo"}},
			}, nil
		},
		SaveConfig: func(raw []byte) ([]string, error) {
			saved = string(raw)
			return nil, nil
		},
	}}
	if w := postModelHidden(t, p, `{"id":"cn:demo","hidden":false}`); w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var doc map[string]any
	_ = json.Unmarshal([]byte(saved), &doc)
	models, _ := doc["models"].(map[string]any)
	if v, has := models["hidden"]; !has || v != nil {
		t.Errorf("清空后 hidden 应为 null（深合并删除），got has=%v v=%v (%s)", has, v, saved)
	}
}

func TestModelHiddenToggleRejectsBadInput(t *testing.T) {
	var saved string
	p := newHiddenTestPanel(&saved)
	for _, body := range []string{
		`{"id":"","hidden":true}`,
		`{"id":"demo","hidden":true}`,    // 缺 realm：裸名无法只关单域
		`{"id":"cn:","hidden":true}`,     // 缺模型
		`{"id":"us:demo","hidden":true}`, // 非法域
		`{"id":`,                         // 坏 JSON
	} {
		if w := postModelHidden(t, p, body); w.Code != http.StatusBadRequest {
			t.Errorf("body=%s status=%d want 400", body, w.Code)
		}
	}
	if saved != "" {
		t.Errorf("非法请求不应调用 SaveConfig, saved=%s", saved)
	}
}

func TestModelHiddenToggleUnavailableWithoutConfigAPI(t *testing.T) {
	p := &Panel{}
	if w := postModelHidden(t, p, `{"id":"cn:demo","hidden":true}`); w.Code != http.StatusNotImplemented {
		t.Errorf("status=%d want 501", w.Code)
	}
}
