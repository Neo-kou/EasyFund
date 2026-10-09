package main

import (
	"encoding/json"
	"fmt"
)

type DanjuanInfo struct {
	FullName        string
	SubscribeStatus string
	CanBuy          bool
	UnitNav         string
	EndDate         string
	DeclareRate     string
	DeclareDiscount string
}

// probeDanjuan 蛋卷基金(雪球旗下持牌销售机构)的公开 djapi。
// 定位: 备源。注意它没有"单日申购限额"字段, 只能做状态交叉验证, 不能替代主源。
// subscribe_status 字段语义未在官方文档中说明, spike 用 040046(限大额) 与 000055(暂停申购) 对照推断。
func probeDanjuan(code string) (*DanjuanInfo, error) {
	body, err := fetchRaw("https://danjuanfunds.com/djapi/fund/" + code)
	if err != nil {
		return nil, err
	}
	var dj struct {
		Data struct {
			FdFullName      string `json:"fd_full_name"`
			SubscribeStatus string `json:"subscribe_status"`
			CanBuy          bool   `json:"can_buy"`
			FundDerived     struct {
				UnitNav string `json:"unit_nav"`
				EndDate string `json:"end_date"`
			} `json:"fund_derived"`
			FundRates struct {
				DeclareRate     string `json:"declare_rate"`
				DeclareDiscount string `json:"declare_discount"`
			} `json:"fund_rates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &dj); err != nil {
		return nil, fmt.Errorf("解析失败: %w (响应前100字节: %.100s)", err, body)
	}
	d := dj.Data
	return &DanjuanInfo{
		FullName:        d.FdFullName,
		SubscribeStatus: d.SubscribeStatus,
		CanBuy:          d.CanBuy,
		UnitNav:         d.FundDerived.UnitNav,
		EndDate:         d.FundDerived.EndDate,
		DeclareRate:     d.FundRates.DeclareRate,
		DeclareDiscount: d.FundRates.DeclareDiscount,
	}, nil
}
