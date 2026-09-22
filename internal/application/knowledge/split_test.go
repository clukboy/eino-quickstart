package knowledge

import (
	"strings"
	"testing"

	"eino-quickstart/internal/rag/constant"
)

// 一份典型的多产品型录：两块之间只靠 ```` ```yaml ```` 围栏分隔。
const twoProductCatalog = "```yaml\n" +
	"product_id: \"H105P\"\n" +
	"model: \"H105P\"\n" +
	"series_name: \"图冠系列\"\n" +
	"specs_from_doc:\n" +
	"  adjust_type: \"偏心轮\"\n" +
	"  open_angle_deg: 100\n" +
	"```\n" +
	"\n" +
	"## H105P 产品简介\n" +
	"\n" +
	"H105P 是二段力小偏心轮快装缓冲铰链。\n" +
	"\n" +
	"```yaml\n" +
	"product_id: \"H105G\"\n" +
	"model: \"H105G\"\n" +
	"series_name: \"图冠系列\"\n" +
	"```\n" +
	"\n" +
	"## H105G 产品简介\n" +
	"\n" +
	"H105G 是固装铰链。\n"

// buildProductDocuments 的产物里，落进 documents.content 的必须是**去掉 YAML 头
// 的正文**，而 YAML 头里的业务键一个都不能丢 —— 它要整份进 documents.metadata。
//
// 两件事一起守，是因为它们很容易被反向改坏：把整块（含 YAML）塞进 content 能
// 让上面的元数据断言照样通过，而代价是同一批型号词在索引里被计两次词频，把
// 「按型号搜」的排序带偏（中文业务词 IDF 接近零，多出来的词频反而主导排序）。
func TestBuildProductDocumentsStripsFrontMatterFromContent(t *testing.T) {
	specs, err := buildProductDocuments(twoProductCatalog, "型录")
	if err != nil {
		t.Fatalf("拆分失败: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("应拆出 2 个产品，实际 %d", len(specs))
	}

	first := specs[0]
	if strings.Contains(first.Content, "```") {
		t.Fatalf("正文里不该留下 YAML 围栏: %q", first.Content)
	}
	if strings.Contains(first.Content, "product_id") {
		t.Fatalf("正文里不该留下 YAML 头: %q", first.Content)
	}
	if !strings.HasPrefix(first.Content, "## H105P 产品简介") {
		t.Fatalf("正文应当是围栏之后的 Markdown，实际 %q", first.Content)
	}

	// YAML 头的内容一个字都不能少，全在元数据上。
	if got := first.Metadata[constant.MetaModel]; got != "H105P" {
		t.Fatalf("model 元数据丢失: %#v", got)
	}
	if got := first.Metadata[constant.MetaSeriesName]; got != "图冠系列" {
		t.Fatalf("series_name 元数据丢失: %#v", got)
	}
	specs1, ok := first.Metadata[constant.MetaSpecsFromDoc].(map[string]any)
	if !ok {
		t.Fatalf("specs_from_doc 应当是嵌套对象，实际 %#v", first.Metadata[constant.MetaSpecsFromDoc])
	}
	if got := specs1["adjust_type"]; got != "偏心轮" {
		t.Fatalf("规格明细丢失: %#v", got)
	}

	if specs[1].Key != "H105G" {
		t.Fatalf("第二个产品的键应为 H105G，实际 %q", specs[1].Key)
	}
}

// 指纹要回答的是「这次重传的内容和一个版本比变了没有」，所以它必须同时看得见
// 正文和 YAML 头。
//
// 只看正文会漏掉「只改了 YAML 头」的那一类重传：补一个规格、改一个系列名时正文
// 一个字没变，于是元数据永远停在旧值上，而症状是「按新系列名搜不到这个产品」——
// 从外部完全看不出索引其实没更新。
func TestProductFingerprintTracksFrontMatterChanges(t *testing.T) {
	baseline := specsOf(t, twoProductCatalog)[0]

	cases := []struct {
		name    string
		content string
		want    bool // 期望与基线相同
	}{
		{
			name:    "原样重传",
			content: twoProductCatalog,
			want:    true,
		},
		{
			// 键顺序、缩进、注释都不改变含义，整理一次格式不该触发全库重切。
			name: "只调整了 YAML 的写法",
			content: "```yaml\n" +
				"series_name: \"图冠系列\"\n" +
				"# 型号是查重键\n" +
				"model:      \"H105P\"\n" +
				"product_id: \"H105P\"\n" +
				"specs_from_doc:\n" +
				"  open_angle_deg: 100\n" +
				"  adjust_type: \"偏心轮\"\n" +
				"```\n" +
				"\n" +
				"## H105P 产品简介\n" +
				"\n" +
				"H105P 是二段力小偏心轮快装缓冲铰链。\n" +
				"\n" +
				"```yaml\n" +
				"product_id: \"H105G\"\n" +
				"model: \"H105G\"\n" +
				"series_name: \"图冠系列\"\n" +
				"```\n" +
				"\n" +
				"## H105G 产品简介\n" +
				"\n" +
				"H105G 是固装铰链。\n",
			want: true,
		},
		{
			name: "只改了 YAML 头里的系列名",
			content: strings.Replace(twoProductCatalog,
				`series_name: "图冠系列"`, `series_name: "图冠二代"`, 1),
			want: false,
		},
		{
			name: "只改了正文",
			content: strings.Replace(twoProductCatalog,
				"H105P 是二段力小偏心轮快装缓冲铰链。",
				"H105P 是二段力小偏心轮快装缓冲铰链，门板厚度 15-28mm。", 1),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := specsOf(t, tc.content)[0]
			same := got.Fingerprint == baseline.Fingerprint
			if same != tc.want {
				t.Fatalf("指纹相同 = %v，期望 %v", same, tc.want)
			}
		})
	}
}

func specsOf(t *testing.T, content string) []productDocument {
	t.Helper()
	specs, err := buildProductDocuments(content, "型录")
	if err != nil {
		t.Fatalf("拆分失败: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("没有拆出任何产品")
	}
	return specs
}

// 指纹必须同时受正文与元数据影响，而且必须稳定。
//
// 三者缺一都有具体的坏结果：不稳定 → 每次重传都判成「变了」，白重切整库；
// 不受正文影响 → 改了正文却不重组索引；不受元数据影响 → 改了 YAML 头却不更新
// 元数据（正是上面那条测试守的形态）。
func TestSpecFingerprintCoversBodyAndMetadata(t *testing.T) {
	body := "## H105P\n\n正文"
	metadata := map[string]any{"model": "H105P"}

	if specFingerprint(body, metadata) != specFingerprint(body, metadata) {
		t.Fatal("同一个块的指纹必须稳定")
	}
	if specFingerprint(body, metadata) == specFingerprint(body+"追加", metadata) {
		t.Fatal("正文变了指纹必须跟着变")
	}
	if specFingerprint(body, metadata) == specFingerprint(body, map[string]any{"model": "H105G"}) {
		t.Fatal("元数据变了指纹必须跟着变")
	}
}
