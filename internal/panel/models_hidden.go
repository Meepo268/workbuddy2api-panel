// models_hidden.go 「模型能力」页行内控制模型是否对下游暴露（写 models.hidden），
// 与配置页的「模型黑名单」文本框同源，保存即热生效。
//
// 为什么复用 models.hidden 而不是另存一份：/v1/models、点单校验与面板开关必须
// 共用一个权威名单，否则会出现「面板显示隐藏、实际仍能调」的漂移。
package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// modelHidden 报告面板行（realm:模型ID）当前是否被 models.hidden 隐藏。
func (p *Panel) modelHidden(realm, id string) bool {
	if p.cfg.Live == nil {
		return false
	}
	_, hidden := p.cfg.Live.Load().HiddenModels[realm+":"+id]
	return hidden
}

// applyHiddenFlags 给面板模型列表补 hidden 标记。列表本身不过滤——被隐藏的模型
// 仍要显示出来，否则用户没法在这里把它重新打开。
func (p *Panel) applyHiddenFlags(entries []map[string]any) {
	if p.cfg.Live == nil {
		return
	}
	hidden := p.cfg.Live.Load().HiddenModels
	if len(hidden) == 0 {
		return
	}
	for _, e := range entries {
		id, _ := e["id"].(string)
		if id == "" {
			continue
		}
		if _, ok := hidden[id]; ok {
			e["hidden"] = true
		}
	}
}

// setModelHidden 在 hidden 列表上增删一条精确 id。
//
// 语义与 livecfg.NormalizeHiddenModels 对齐：带 "cn:"/"global:" 前缀的条目只影响
// 该域，裸名同时影响两域。因此关闭某一域时，裸名条目要拆成另一个域的精确条目，
// 否则会把另一域也一起放出来。
func setModelHidden(entries []string, id string, hidden bool) []string {
	realm, bare, ok := strings.Cut(id, ":")
	if !ok || (realm != "cn" && realm != "global") || bare == "" {
		return entries
	}
	out := make([]string, 0, len(entries)+1)
	seen := make(map[string]struct{}, len(entries)+1)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	if hidden {
		for _, e := range entries {
			add(e)
		}
		add(id)
		return out
	}

	other := "global"
	if realm == "global" {
		other = "cn"
	}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		switch e {
		case "", id:
			continue
		case bare: // 裸名覆盖两域：只关当前域，另一域改写为精确条目
			add(other + ":" + bare)
		default:
			add(e)
		}
	}
	return out
}

// modelHiddenToggle 设置/清除单模型的下游可见性。id 必须是 cn:模型ID 或
// global:模型ID（面板行 id 原样回传），裸名无法表达「只隐藏某一域」。
func (p *Panel) modelHiddenToggle(w http.ResponseWriter, r *http.Request) {
	if p.cfg.LoadConfig == nil || p.cfg.SaveConfig == nil {
		writeErr(w, http.StatusNotImplemented, "config api not available")
		return
	}
	var req struct {
		ID     string `json:"id"`
		Hidden bool   `json:"hidden"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	id := strings.TrimSpace(req.ID)
	realm, bare := upstream.SplitRealmPrefix(id)
	if id == bare || bare == "" || (realm != "cn" && realm != "global") {
		writeErr(w, http.StatusBadRequest, "id 需为 cn:模型ID 或 global:模型ID")
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
	// 避免 float64 把大整数写成 1e+06（与 models_thinking 同一处理）。
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		writeErr(w, http.StatusInternalServerError, "decode config: "+err.Error())
		return
	}
	models, _ := doc["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
		doc["models"] = models
	}
	var entries []string
	switch cur := models["hidden"].(type) {
	case []any:
		for _, v := range cur {
			if s, ok := v.(string); ok {
				entries = append(entries, s)
			}
		}
	case []string:
		entries = append(entries, cur...)
	}
	next := setModelHidden(entries, id, req.Hidden)
	if len(next) == 0 {
		models["hidden"] = nil // null 才能让深合并删掉旧数组；空数组会覆盖成 []
	} else {
		models["hidden"] = next
	}

	out, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal config: "+err.Error())
		return
	}
	restartRequired, err := p.cfg.SaveConfig(out)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if restartRequired == nil {
		restartRequired = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"id":               id,
		"hidden":           req.Hidden,
		"restart_required": restartRequired,
	})
}
