package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are prompt-contract regressions, not proof of factual model output.
// Actual output still needs quantity checks and an editorial review.
func TestEditorialPromptContracts(t *testing.T) {
	for _, stage := range []string{"collect", "expand", "dedupe"} {
		t.Run(stage, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(packDir(t), "prompts", stage+".md"))
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			for _, required := range []string{"预算", "平台", "500", "未提供可核实来源", "不编造", "需核实", "问号不能消除标题里的事实预设"} {
				if !strings.Contains(body, required) {
					t.Errorf("missing contract %q", required)
				}
			}
			if stage != "collect" {
				for _, field := range []string{"original_request", "previous_stage_output"} {
					if !strings.Contains(body, field) {
						t.Errorf("missing chain envelope field %q", field)
					}
				}
			}
			for _, obsolete := range []string{"用具体小区名、具体年份、具体金额或具体面积", "30 万首付能在亚运村买到", "https://example.com/v1/agents"} {
				if strings.Contains(body, obsolete) {
					t.Errorf("unsafe obsolete example %q", obsolete)
				}
			}
		})
	}
}

func TestEditorialActionContracts(t *testing.T) {
	for _, stage := range []string{"generate_post", "generate_video"} {
		data, err := os.ReadFile(filepath.Join(packDir(t), "prompts", stage+".md"))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		for _, required := range []string{"payload.original_request", "payload.original_title", "本次有效选题", "一次现场清洁状态不能证明响应速度", "未完成记录单独标注", "危险隐患立即报告", "小红书", "预算", "未提供可核实来源", "不编造", "需核实", "已保存选题也可能包含未经核实的前提"} {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing %q", stage, required)
			}
		}
		for _, obsolete := range []string{"运营了 50 万粉", "200 万播放", "三选二", "调用方会触发重生成", "提一句\"链家成交\""} {
			if strings.Contains(body, obsolete) {
				t.Errorf("%s unsafe %q", stage, obsolete)
			}
		}
	}
}
