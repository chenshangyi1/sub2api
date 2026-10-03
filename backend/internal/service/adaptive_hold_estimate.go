package service

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// EstimateAdaptiveHoldBaseFromPreauthorization prices Adaptive HOLD using the
// same request-local token estimate and unified cost calculator as v247 text
// preauthorization. Authorize then adds ManagementFeeBPS on top of this base.
func EstimateAdaptiveHoldBaseFromPreauthorization(calculator *BillingService, costInput CostInput, body []byte) (decimal.Decimal, error) {
	if calculator == nil {
		return decimal.Zero, ErrInvalidBillingPreauthorizationEstimate
	}
	tokens := EstimateBalancePreauthorizationTokens(body)
	costInput.Tokens = UsageTokens{
		InputTokens:  tokens.InputTokens,
		OutputTokens: tokens.OutputTokens,
	}
	breakdown, err := calculator.CalculateCostUnified(costInput)
	if err != nil {
		return decimal.Zero, err
	}
	if breakdown == nil || invalidNonnegativeMoney(breakdown.ActualCost) || breakdown.ActualCost == 0 {
		return decimal.Zero, ErrInvalidBillingPreauthorizationEstimate
	}
	held := quantizeBillingHoldUpFromFloat(breakdown.ActualCost)
	if invalidNonnegativeMoney(held) || held <= 0 {
		return decimal.Zero, ErrInvalidBillingPreauthorizationEstimate
	}
	return decimal.NewFromFloat(held).Round(AdaptiveBillingMoneyScale), nil
}

// EstimateAdaptiveHoldBase prices Adaptive HOLD for an OpenAI-compatible
// inbound using v247 BalancePreauthorizationCostInput, then the shared token
// estimate. rateMultiplier, when positive, overrides the group rate so HOLD
// covers the most expensive Adaptive leaf.
func (s *OpenAIGatewayService) EstimateAdaptiveHoldBase(
	ctx context.Context,
	apiKey *APIKey,
	body []byte,
	model string,
	pricingAt time.Time,
	serviceTier string,
	rateMultiplier float64,
) (decimal.Decimal, error) {
	if s == nil {
		return decimal.Zero, ErrInvalidBillingPreauthorizationEstimate
	}
	input := s.BalancePreauthorizationCostInput(ctx, apiKey, model, pricingAt, serviceTier)
	if rateMultiplier > 0 {
		input.RateMultiplier = rateMultiplier
	}
	return EstimateAdaptiveHoldBaseFromPreauthorization(s.billingService, input, body)
}

// EstimateAdaptiveHoldBase prices Adaptive HOLD for Anthropic/Gemini inbound
// using the same v247 text preauthorization estimator as OpenAI.
func (s *GatewayService) EstimateAdaptiveHoldBase(
	ctx context.Context,
	apiKey *APIKey,
	body []byte,
	model string,
	pricingAt time.Time,
	serviceTier string,
	rateMultiplier float64,
) (decimal.Decimal, error) {
	if s == nil {
		return decimal.Zero, ErrInvalidBillingPreauthorizationEstimate
	}
	input := s.BalancePreauthorizationCostInput(ctx, apiKey, model, pricingAt, serviceTier)
	if rateMultiplier > 0 {
		input.RateMultiplier = rateMultiplier
	}
	return EstimateAdaptiveHoldBaseFromPreauthorization(s.billingService, input, body)
}
