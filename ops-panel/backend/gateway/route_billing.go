// 直连路由账号计费倍率落库规则。
package gateway

import (
	"github.com/bejix/upstream-ops/backend/pkg/rateconvert"
	"github.com/bejix/upstream-ops/backend/storage"
)

func (svc *Service) applyProviderRouteBilling(route *storage.GatewayRoute, p *storage.GatewayProvider) {
	if route == nil || p == nil {
		return
	}
	def := p.DefaultBillingRate
	if def <= 0 {
		def = 1
	}
	mode := rateconvert.NormalizeMode(route.RateConvertMode)
	if mode == "custom" {
		if route.RateConvertValue <= 0 {
			route.RateConvertValue = def
		}
		// custom 以 convert value 为准，保证调度与计费一致
		route.BillingRateMultiplier = route.RateConvertValue
		return
	}
	route.BillingRateMultiplier = def
	if route.RateConvertValue <= 0 {
		route.RateConvertValue = 1
	}
}
