package panel

import (
	"os"
	"strings"
	"testing"
)

// TestThinkingPriorityUiWiring 优先模式的「下拉 → 隐藏 checkbox」接线不能断。
//
// 为什么需要：配置结构里 thinking.client_priority 仍是布尔，界面上换成了
// 客户端优先/网关优先下拉；下拉只改写隐藏 checkbox，collectConfig 再照旧读
// checkbox。接线一断，保存出去的仍是旧布尔值，用户看到「已保存」但网关行为
// 不变——正是「勾了客户端优先、日志却像网关优先」的形态。
func TestThinkingPriorityUiWiring(t *testing.T) {
	htmlBytes, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, want := range []string{
		`id="thinkingPriorityUi"`,
		`id="thinkingClientPriority"`,
		`name="thinking_client_priority"`,
		`switch inline`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html 缺少 %q（优先模式下拉/隐藏 checkbox/开关内联）", want)
		}
	}
	if strings.Contains(html, `type="checkbox" name="thinking_client_priority"></div>`) {
		t.Error("thinking_client_priority 仍是右对齐开关；应隐藏并由 thinkingPriorityUi 下拉驱动")
	}

	jsBytes, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(jsBytes)
	for _, want := range []string{
		"function syncThinkingPriorityUi()",
		"syncThinkingPriorityUi();",
		"ui.value = cb.checked ? 'client' : 'gateway'",
		"cb.checked = $('thinkingPriorityUi').value === 'client'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js 缺少 %q（下拉与 checkbox 双向同步）", want)
		}
	}
}
