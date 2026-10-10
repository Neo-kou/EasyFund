// Package tag 基金标签推导: 公司/指数/份额/特征, 全部从名称+接口类型确定性推导, 无网络请求。
// 字典基于 2026-10-10 全市场批量接口实测分布(QDII+海外指数池 738 只)整理, 未命中兜底 "其他"。
package tag

import "strings"

// 基金公司简称(名称前缀)。长的在前, 避免短名抢占(如 "富国" vs "国海富兰克林")。
// 注意: 场内 ETF 简称多为 "指数+ETF"(如 纳指ETF/恒生科技ETF), 无公司前缀, 命中不了就打不出公司标签(优于误标 "其他")。
var companies = []string{
	"国海富兰克林", "华泰柏瑞", "景顺长城", "汇丰晋信", "信达澳亚", "民生加银",
	"国投瑞银", "上投摩根", "摩根", "汇添富", "易方达", "工银瑞信", "工银",
	"兴证全球", "交银施罗德", "交银", "建信", "银华", "鹏华", "招商", "海富通",
	"长信", "诺安", "宏利", "中银", "浦银安盛", "长城", "华富", "宝盈",
	"平安", "永赢", "中欧", "睿远", "广发", "华夏", "嘉实", "富国", "南方",
	"华安", "博时", "大成", "天弘", "华宝", "国泰", "万家", "东方红", "财通",
	"浙商", "中金", "银河", "申万菱信", "泰康", "国寿安保", "中信保诚", "光大保德信",
	"国富", "融通", "创金合信", "光大阳光", "恒生前海",
}

// 指数/主题标签: {匹配词列表, 标签}, 从上到下先匹配先赢(具体的在前)。
var indexRules = []struct {
	keys []string
	tag  string
}{
	{[]string{"纳斯达克100", "纳指100", "纳指ETF"}, "纳斯达克100"},
	{[]string{"纳斯达克科技", "纳指科技"}, "纳斯达克科技"},
	{[]string{"纳斯达克"}, "纳斯达克综合"},
	{[]string{"标普信息科技"}, "标普信息科技"},
	{[]string{"标普生物科技"}, "标普生物科技"},
	{[]string{"标普500", "标准普尔500", "标普500ETF"}, "标普500"},
	{[]string{"标普"}, "标普其他"},
	{[]string{"道琼斯"}, "道琼斯"},
	{[]string{"恒生科技"}, "恒生科技"},
	{[]string{"恒生中国企业", "H股"}, "恒生国企"},
	{[]string{"恒生"}, "恒生指数"},
	{[]string{"中概互联"}, "中概互联网"},
	{[]string{"港股通", "港股", "香港"}, "港股"},
	{[]string{"大中华", "中国概念", "中国"}, "大中华"},
	{[]string{"新兴市场"}, "新兴市场"},
	{[]string{"互联网"}, "互联网"},
	{[]string{"日经"}, "日经225"},
	{[]string{"德国", "DAX"}, "德国DAX"},
	{[]string{"法国", "CAC"}, "法国CAC40"},
	{[]string{"英国", "富时100"}, "富时100"},
	{[]string{"印度"}, "印度"},
	{[]string{"越南"}, "越南"},
	{[]string{"东南亚"}, "东南亚"},
	{[]string{"亚太"}, "亚太"},
	{[]string{"美国房地产", "美国REIT", "房地产"}, "美国REITs"},
	{[]string{"全球医疗", "医疗保健"}, "全球医疗"},
	{[]string{"医药", "生物"}, "医药生物"},
	{[]string{"黄金"}, "黄金"},
	{[]string{"原油", "石油", "油气"}, "原油油气"},
	{[]string{"白银"}, "白银"},
	{[]string{"债"}, "海外债券"},
	{[]string{"全球"}, "全球其他"},
}

// Company 名称前缀→基金公司简称; 未命中返回 ""(场内 ETF 等无公司名称的情形, 不硬贴 "其他")。
func Company(name string) string {
	for _, c := range companies {
		if strings.HasPrefix(name, c) {
			if c == "上投摩根" || c == "工银" {
				return map[string]string{"上投摩根": "摩根", "工银": "工银瑞信"}[c]
			}
			return c
		}
	}
	return ""
}

// Index 名称→跟踪指数/主题标签; 未命中返回 "其他"。
func Index(name string) string {
	for _, r := range indexRules {
		for _, k := range r.keys {
			if strings.Contains(name, k) {
				return r.tag
			}
		}
	}
	return "其他"
}

// Shares 名称→份额标签(0~2 个): A/C/E 份额、美元现汇/现钞、LOF。
func Shares(name string) []string {
	var out []string
	switch {
	case strings.Contains(name, "美元现汇"):
		out = append(out, "美元现汇")
	case strings.Contains(name, "美元现钞"):
		out = append(out, "美元现钞")
	case strings.Contains(name, "美元"):
		out = append(out, "美元")
	}
	if strings.Contains(name, "LOF") {
		out = append(out, "LOF")
	}
	// 份额字母通常在名称末尾: ...A / ...C / ...人民币A
	for _, s := range []string{"A", "C", "E", "I", "D"} {
		if strings.HasSuffix(name, s) {
			out = append(out, s)
			break
		}
	}
	return out
}

// Derive 全量标签: 公司(可为空) + 指数 + 份额 + 接口类型原值 + 特征(ETF联接/场内ETF/REITs)。
func Derive(name, apiType, statusRaw string) []string {
	var tags []string
	if c := Company(name); c != "" {
		tags = append(tags, c)
	}
	tags = append(tags, Index(name))
	tags = append(tags, Shares(name)...)
	if apiType != "" {
		tags = append(tags, apiType)
	}
	if strings.Contains(name, "ETF联接") {
		tags = append(tags, "ETF联接")
	}
	if statusRaw == "场内交易" {
		tags = append(tags, "场内ETF")
	}
	if strings.Contains(name, "REIT") {
		tags = append(tags, "REITs")
	}
	return tags
}
