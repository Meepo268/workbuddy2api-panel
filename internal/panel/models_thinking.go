// models_thinking.go 「模型能力」页行内编辑思考默认档：写 thinking.model_defaults，
// 走与配置页相同的 SaveConfig 闭包（校验 → 落盘 → 热应用），保存即生效。
//
// 只改这一个键：先 LoadConfig 取当前配置 → 反序列化成 map → 增删
// thinking.model_defaults["realm:model"] → SaveConfig 整份回写。这样环境变量托管、
// 并发保存、restart 字段判定全部沿用既有逻辑，不另造一套配置读写。
package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// modelThinking 设置/清除单模型默认档。effort 为空或 "inherit" 表示清除覆盖。
func (p *Panel) modelThinking(w http.ResponseWriter, r *http.Request) {
	if p.cfg.LoadConfig == nil || p.cfg.SaveConfig == nil {
		writeErr(w, http.StatusNotImplemented, "config api not available")
		return
	}
	var req struct {
		ID     string `json:"id"`
		Effort string `json:"effort"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	realm, bare := upstream.SplitRealmPrefix(strings.TrimSpace(req.ID))
	if bare == "" {
		writeErr(w, http.StatusBadRequest, "id 需为 模型ID、cn:模型ID 或 global:模型ID")
		return
	}
	effort := strings.ToLower(strings.TrimSpace(req.Effort))
	if effort != "" && effort != "inherit" && !upstream.IsEffortLevel(effort) {
		writeErr(w, http.StatusBadRequest, "effort 需为 inherit 或 off/minimal/low/medium/high/xhigh/max")
		return
	}

	cfgAny, err := p.cfg.LoadConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	raw, err := json.Marshal(cfgAny)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal config: "+err.Error())
		return
	}
	// UseNumber：配置里 json.Number 交给 SaveConfig 的 ParseConfig 重新校验，
	// 避免 float64 把大整数（context/额度）写成 1e+06。
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		writeErr(w, http.StatusInternalServerError, "decode config: "+err.Error())
		return
	}
	thinking, _ := doc["thinking"].(map[string]any)
	if thinking == nil {
		thinking = map[string]any{}
		doc["thinking"] = thinking
	}
	defaults, _ := thinking["model_defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	key := realm + ":" + bare
	if effort == "" || effort == "inherit" {
		delete(defaults, key)
	} else {
		defaults[key] = effort
	}
	if len(defaults) == 0 {
		delete(thinking, "model_defaults")
	} else {
		thinking["model_defaults"] = defaults
	}

	out, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal config: "+err.Error())
		return
	}
	restart, err := p.cfg.SaveConfig(out)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if restart == nil {
		restart = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"id":               key,
		"effort":           effort,
		"restart_required": restart,
	})
}
