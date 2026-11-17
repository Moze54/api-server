package spiders

import (
	"strings"
	"testing"
)

// xzw 白羊座页面 2026-11-17 实测结构裁剪样例。
const fixtureXZW = `<dl><dt><span><img src="/1.png" title="白羊座"></span></dt>
<dd><h4>白羊座今日运势<small>11月17日</small></h4><ul>
<li><label>综合运势：</label><span class="star_m star_blue"><em style=" width:64px;"></em></em></li>
<li><label>爱情运势：</label><span class="star_m star_blue"><em style=" width:48px;"></em></em></li>
<li><label>健康指数：</label>68%</li>
<li><label>幸运颜色：</label>橘黄</li>
<li><label>幸运数字：</label>12</li>
<li><label>速配星座：</label>狮子座</li>
<li class="desc"><label>短评：</label>打磨自身能力</li></ul></dd></dl>
<div class="c_cont"><p><strong class="p1">综合运势</strong><span>整体运势处于平稳上升的状态。</span></p></div>`

func TestParseHoroscope(t *testing.T) {
	out, err := parseHoroscope([]byte(fixtureXZW), "aries")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.Name != "白羊座" || out.Date != "11月17日" {
		t.Fatalf("名称/日期错误: %+v", out)
	}
	if out.Fortune["综合运势"] != 4 || out.Fortune["爱情运势"] != 3 {
		t.Fatalf("星级换算错误(64px→4, 48px→3): %+v", out.Fortune)
	}
	if out.Index["健康指数"] != "68" {
		t.Fatalf("指数错误: %+v", out.Index)
	}
	if out.Lucky["幸运颜色"] != "橘黄" || out.Lucky["幸运数字"] != "12" ||
		out.Lucky["速配星座"] != "狮子座" || out.Lucky["短评"] != "打磨自身能力" {
		t.Fatalf("幸运字段错误: %+v", out.Lucky)
	}
	if out.Summary["综合运势"] == "" {
		t.Fatalf("概述缺失: %+v", out.Summary)
	}
}

func TestStripHTML(t *testing.T) {
	in := `清朝摄政王<a href="https://baike.baidu.com/item/x">多尔衮</a>出生`
	got := stripHTML(in)
	if got != "清朝摄政王多尔衮出生" {
		t.Fatalf("去标签失败: %q", got)
	}
	got2 := stripHTML("&#20013;&#22269;成立")
	if got2 != "中国成立" {
		t.Fatalf("实体解码失败: %q", got2)
	}
}

func TestAstroAlias(t *testing.T) {
	if astroAliases["白羊座"] != "aries" || astroAliases["leo"] != "leo" {
		t.Fatal("星座别名表错误")
	}
}

func TestFormatDate8(t *testing.T) {
	if formatDate8("20261117") != "2026-11-17" {
		t.Fatal("日期格式化错误")
	}
	if formatDate8("x") != "x" {
		t.Fatal("异常长度应原样返回")
	}
	if strings.Repeat("a", 1) == "" {
		t.Fatal("占位")
	}
}
