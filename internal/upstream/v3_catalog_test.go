// v3_catalog_test.go /v3/config 名单口径：data.models 是全量能力目录，
// 上架名单以 agents[cli].models ∪ 试用横幅为准（issue：列出 53 个但 20 个可用）。
package upstream

import (
	"net/http"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

const v3CatalogFixture = `{"code":0,"data":{
  "models":[
    {"id":"live-a","name":"Live A","maxOutputTokens":8000},
    {"id":"live-b","name":"Live B","maxOutputTokens":8000},
    {"id":"legacy-old","name":"Legacy","maxOutputTokens":8000},
    {"id":"tgt-preview","name":"Preview","maxOutputTokens":8000,"credits":"0.29","tags":["craft"],
     "reasoning":{"supportedEfforts":["low","high"],"defaultEffort":"high"}}
  ],
  "agents":[{"name":"cli","models":["live-a","live-b"]}],
  "productFeaturesConfig":{"modelTrialBanner":{"banners":[
    {"modelId":"trial-x","targetModelId":"tgt-preview","trialDays":14}
  ]}}
}}`

func TestFetchV3ConfigModelMapCatalog(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, v3CatalogFixture), nil
	})
	cat, err := c.fetchV3ConfigModelMap(&auth.Auth{AccessToken: "at", UID: "u1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.CLIIDs) != 2 || cat.CLIIDs[0] != "live-a" || cat.CLIIDs[1] != "live-b" {
		t.Errorf("CLIIDs=%v want [live-a live-b]", cat.CLIIDs)
	}
	if len(cat.TrialIDs) != 1 || cat.TrialIDs[0] != "trial-x" {
		t.Errorf("TrialIDs=%v want [trial-x]", cat.TrialIDs)
	}
	if _, ok := cat.Models["legacy-old"]; !ok {
		t.Errorf("全量能力表应保留 legacy-old（只做元数据，不进上架名单）")
	}
	trial, ok := cat.Models["trial-x"]
	if !ok {
		t.Fatalf("试用横幅模型应补进能力表")
	}
	// 试用版继承转正目标的能力，但计费/营销字段清空。
	if len(trial.Efforts) != 2 || trial.Credits != "" || len(trial.Tags) != 0 {
		t.Errorf("trial-x=%+v want 继承 efforts、清空 credits/tags", trial)
	}
}

func TestFetchV3ModelsFiltersByListing(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, v3CatalogFixture), nil
	})
	infos, err := c.fetchV3Models(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, mi := range infos {
		ids = append(ids, mi.ID)
	}
	got := strings.Join(ids, ",")
	if got != "live-a,live-b,trial-x" {
		t.Errorf("上架名单=%s want live-a,live-b,trial-x（legacy-old 必须被过滤）", got)
	}
}

func TestFetchV3ModelsFallsBackToFullCatalogWithoutAgents(t *testing.T) {
	fixture := strings.Replace(v3CatalogFixture, `"agents":[{"name":"cli","models":["live-a","live-b"]}],`, "", 1)
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, fixture), nil
	})
	infos, err := c.fetchV3Models(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, mi := range infos {
		seen[mi.ID] = true
	}
	if !seen["legacy-old"] || !seen["live-a"] {
		t.Errorf("名单缺失应退回全量目录，got %v", seen)
	}
}
