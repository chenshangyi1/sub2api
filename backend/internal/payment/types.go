// Package payment provides the core payment provider abstraction,
// registry, load balancing, and shared utilities for the payment subsystem.
package payment

import (
	"context"
	"encoding/json"
	"strings"
)

// PaymentType represents a supported payment method.
type PaymentType = string

// Supported payment type constants.
const (
	TypeAlipay       PaymentType = "alipay"
	TypeWxpay        PaymentType = "wxpay"
	TypeAlipayDirect PaymentType = "alipay_direct"
	TypeWxpayDirect  PaymentType = "wxpay_direct"
	TypeStripe       PaymentType = "stripe"
	TypeCard         PaymentType = "card"
	TypeLink         PaymentType = "link"
	TypeEasyPay      PaymentType = "easypay"
	TypeAirwallex    PaymentType = "airwallex"
	TypeEpusdt       PaymentType = "epusdt"
)

// Order status constants shared across payment and service layers.
const (
	OrderStatusPending           = "PENDING"
	OrderStatusPaid              = "PAID"
	OrderStatusRecharging        = "RECHARGING"
	OrderStatusCompleted         = "COMPLETED"
	OrderStatusExpired           = "EXPIRED"
	OrderStatusCancelled         = "CANCELLED"
	OrderStatusFailed            = "FAILED"
	OrderStatusRefundRequested   = "REFUND_REQUESTED"
	OrderStatusRefunding         = "REFUNDING"
	OrderStatusRefundPending     = "REFUND_PENDING"
	OrderStatusPartiallyRefunded = "PARTIALLY_REFUNDED"
	OrderStatusRefunded          = "REFUNDED"
	OrderStatusRefundFailed      = "REFUND_FAILED"
)

// Order types distinguish balance recharges from subscription purchases.
const (
	OrderTypeBalance      = "balance"
	OrderTypeSubscription = "subscription"
)

// Entity statuses shared across users, groups, etc.
const (
	EntityStatusActive = "active"
)

// Deduction types for refund flow.
const (
	DeductionTypeBalance      = "balance"
	DeductionTypeSubscription = "subscription"
	DeductionTypeNone         = "none"
)

// Payment notification status values.
const (
	NotificationStatusSuccess = "success"
	NotificationStatusPaid    = "paid"
)

// Provider-level status constants returned by provider implementations
// to the service layer (lowercase, distinct from OrderStatus uppercase constants).
const (
	ProviderStatusPending  = "pending"
	ProviderStatusPaid     = "paid"
	ProviderStatusSuccess  = "success"
	ProviderStatusFailed   = "failed"
	ProviderStatusRefunded = "refunded"
)

// DefaultLoadBalanceStrategy is the default load-balancing strategy
// used when no strategy is configured.
const DefaultLoadBalanceStrategy = "round-robin"

// ConfigKeyPublishableKey is the config map key for Stripe's publishable key.
const ConfigKeyPublishableKey = "publishableKey"

// GetBasePaymentType extracts the base payment method from a composite key.
// For example, "alipay_direct" -> "alipay".
func GetBasePaymentType(t string) string {
	switch {
	case t == TypeEasyPay:
		return TypeEasyPay
	case t == TypeAirwallex:
		return TypeAirwallex
	case t == TypeEpusdt || IsEPUSDTCheckoutMethod(t):
		return TypeEpusdt
	case t == TypeStripe || t == TypeCard || t == TypeLink:
		return TypeStripe
	case len(t) >= len(TypeAlipay) && t[:len(TypeAlipay)] == TypeAlipay:
		return TypeAlipay
	case len(t) >= len(TypeWxpay) && t[:len(TypeWxpay)] == TypeWxpay:
		return TypeWxpay
	default:
		return t
	}
}

const epusdtCheckoutMethodPrefix = TypeEpusdt + "_"

// IsEPUSDTCheckoutMethod reports whether t is a network-specific EPUSDT checkout key
// such as "epusdt_bsc" or "epusdt_trc20".
func IsEPUSDTCheckoutMethod(t string) bool {
	return strings.HasPrefix(strings.TrimSpace(t), epusdtCheckoutMethodPrefix)
}

// EPUSDTCheckoutNetwork extracts the configured network from a checkout method.
func EPUSDTCheckoutNetwork(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	if !strings.HasPrefix(t, epusdtCheckoutMethodPrefix) {
		return ""
	}
	return strings.TrimSpace(t[len(epusdtCheckoutMethodPrefix):])
}

// EPUSDTCheckoutMethod builds the user-facing checkout key for an EPUSDT network.
func EPUSDTCheckoutMethod(network string) string {
	network = NormalizeEPUSDTNetwork(network)
	if network == "" {
		return TypeEpusdt
	}
	return TypeEpusdt + "_" + network
}

// NormalizeEPUSDTNetwork canonicalizes an EPUSDT network id.
func NormalizeEPUSDTNetwork(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "bsc", "bep20", "bnb", "binance":
		return "bsc"
	case "trc20", "tron", "trx":
		return "trc20"
	case "polygon", "matic", "pos":
		return "polygon"
	case "erc20", "eth", "ethereum":
		return "erc20"
	case "":
		return ""
	default:
		return strings.ToLower(strings.TrimSpace(network))
	}
}

// EPUSDTUpstreamNetwork maps a checkout/admin network id onto the chain id
// expected by GMPay create-transaction (binance/tron/ethereum/polygon).
func EPUSDTUpstreamNetwork(network string) string {
	switch NormalizeEPUSDTNetwork(network) {
	case "bsc":
		return "binance"
	case "trc20":
		return "tron"
	case "erc20":
		return "ethereum"
	case "polygon":
		return "polygon"
	default:
		return strings.ToLower(strings.TrimSpace(network))
	}
}

// EPUSDTNetworkDisplayName is the user-facing chain label shown on checkout.
func EPUSDTNetworkDisplayName(network string) string {
	switch NormalizeEPUSDTNetwork(network) {
	case "bsc":
		return "BNB Smart Chain / BSC (BEP20)"
	case "trc20":
		return "TRON (TRC20)"
	case "polygon":
		return "Polygon (PoS)"
	case "erc20":
		return "Ethereum (ERC20)"
	default:
		return ""
	}
}

// EPUSDTNetworkFromMap reads the first configured network from a decrypted provider config.
func EPUSDTNetworkFromMap(cfg map[string]string) string {
	networks := EPUSDTNetworksFromMap(cfg)
	if len(networks) == 0 {
		return ""
	}
	return networks[0]
}

// EPUSDTNetworksFromMap reads every configured EPUSDT network.
// It prefers the comma-separated "networks" field and falls back to "network".
func EPUSDTNetworksFromMap(cfg map[string]string) []string {
	if cfg == nil {
		return nil
	}
	raw := strings.TrimSpace(cfg["networks"])
	if raw == "" {
		raw = strings.TrimSpace(cfg["network"])
	}
	if raw == "" {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		network := NormalizeEPUSDTNetwork(part)
		if network == "" || seen[network] {
			continue
		}
		seen[network] = true
		out = append(out, network)
	}
	return out
}

// EPUSDTInstanceSupportsNetwork reports whether cfg can serve the checkout network.
func EPUSDTInstanceSupportsNetwork(cfg map[string]string, network string) bool {
	wanted := NormalizeEPUSDTNetwork(network)
	if wanted == "" {
		return false
	}
	for _, got := range EPUSDTNetworksFromMap(cfg) {
		if got == wanted {
			return true
		}
	}
	return false
}

// EPUSDTNetworkFromConfig reads the first network from a stored provider config blob.
func EPUSDTNetworkFromConfig(stored string) string {
	networks := EPUSDTNetworksFromConfig(stored)
	if len(networks) == 0 {
		return ""
	}
	return networks[0]
}

// EPUSDTNetworksFromConfig reads every network from a stored provider config blob.
func EPUSDTNetworksFromConfig(stored string) []string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return nil
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil || cfg == nil {
		return nil
	}
	return EPUSDTNetworksFromMap(cfg)
}

// CreatePaymentRequest holds the parameters for creating a new payment.
type CreatePaymentRequest struct {
	OrderID     string // Internal order ID
	Amount      string // 支付金额，按服务商实例配置的币种解释
	PaymentType string // e.g. "alipay", "wxpay", "stripe"
	Subject     string // Product description
	NotifyURL   string // Webhook callback URL
	ReturnURL   string // Browser redirect URL after payment
	OpenID      string // WeChat JSAPI payer OpenID when available
	ClientIP    string // Payer's IP address
	IsMobile    bool   // Whether the request comes from a mobile device
	// AlipayMobilePrecreate routes a mobile Alipay request through
	// alipay.trade.precreate instead of alipay.trade.wap.pay.
	AlipayMobilePrecreate bool
	InstanceSubMethods    string // Comma-separated sub-methods from instance supported_types (for Stripe)
}

// CreatePaymentResultType describes the shape of the create-payment result.
type CreatePaymentResultType = string

const (
	CreatePaymentResultOrderCreated  CreatePaymentResultType = "order_created"
	CreatePaymentResultOAuthRequired CreatePaymentResultType = "oauth_required"
	CreatePaymentResultJSAPIReady    CreatePaymentResultType = "jsapi_ready"
)

// WechatOAuthInfo describes the next step when WeChat OAuth is required before payment.
type WechatOAuthInfo struct {
	AuthorizeURL string `json:"authorize_url,omitempty"`
	AppID        string `json:"appid,omitempty"`
	OpenID       string `json:"openid,omitempty"`
	Scope        string `json:"scope,omitempty"`
	State        string `json:"state,omitempty"`
	RedirectURL  string `json:"redirect_url,omitempty"`
}

// WechatJSAPIPayload contains the fields the frontend needs to invoke WeChat JSAPI payment.
type WechatJSAPIPayload struct {
	AppID     string `json:"appId,omitempty"`
	TimeStamp string `json:"timeStamp,omitempty"`
	NonceStr  string `json:"nonceStr,omitempty"`
	Package   string `json:"package,omitempty"`
	SignType  string `json:"signType,omitempty"`
	PaySign   string `json:"paySign,omitempty"`
}

// CreatePaymentResponse is returned after successfully initiating a payment.
type CreatePaymentResponse struct {
	TradeNo      string                  // Third-party transaction ID
	PayURL       string                  // H5 payment URL (alipay/wxpay)
	QRCode       string                  // QR code content for scanning
	ClientSecret string                  // Stripe PaymentIntent 客户端密钥
	IntentID     string                  // 前端 SDK 需要的服务商支付意图 ID
	Currency     string                  // 服务商支付币种
	CountryCode  string                  // 服务商收银台国家/地区代码
	PaymentEnv   string                  // 服务商前端环境标识
	ResultType   CreatePaymentResultType // Typed result contract for frontend flows
	OAuth        *WechatOAuthInfo        // WeChat OAuth bootstrap payload when required
	JSAPI        *WechatJSAPIPayload     // WeChat JSAPI invocation payload when ready
}

// QueryOrderResponse describes the payment status from the upstream provider.
type QueryOrderResponse struct {
	TradeNo  string
	Status   string  // "pending", "paid", "failed", "refunded"
	Amount   float64 // 按服务商返回币种解释的金额
	PaidAt   string  // RFC3339 timestamp or empty
	Metadata map[string]string
}

// PaymentNotification is the parsed result of a webhook/notify callback.
type PaymentNotification struct {
	TradeNo  string
	OrderID  string
	Amount   float64
	Status   string // "success" or "failed"
	RawData  string // Raw notification body for audit
	Metadata map[string]string
}

// RefundRequest contains the parameters for requesting a refund.
type RefundRequest struct {
	TradeNo string
	OrderID string
	Amount  string // Refund amount formatted to 2 decimal places
	Reason  string
}

// RefundQueryRequest contains identifiers needed to query a previously
// requested refund.
type RefundQueryRequest struct {
	TradeNo  string
	OrderID  string
	RefundID string
	Amount   string
}

// RefundResponse is returned after a refund request.
type RefundResponse struct {
	RefundID string
	Status   string // "success", "pending", "failed"
}

// InstanceSelection holds the selected provider instance and its decrypted config.
type InstanceSelection struct {
	InstanceID     string
	ProviderKey    string // Provider key of the selected instance (e.g. "alipay", "easypay")
	Config         map[string]string
	SupportedTypes string // Comma-separated list of supported payment types from the instance
	PaymentMode    string // Payment display mode: "qrcode", "redirect", "popup"
	// RechargeFeeRate, when set, overrides the global recharge fee for this instance.
	RechargeFeeRate *float64
	// BalanceRechargeMultiplier, when set, overrides the global balance multiplier.
	BalanceRechargeMultiplier *float64
}

// Provider defines the interface that all payment providers must implement.
type Provider interface {
	// Name returns a human-readable name for this provider.
	Name() string
	// ProviderKey returns the unique key identifying this provider type (e.g. "easypay").
	ProviderKey() string
	// SupportedTypes returns the list of payment types this provider handles.
	SupportedTypes() []PaymentType
	// CreatePayment initiates a payment and returns the upstream response.
	CreatePayment(ctx context.Context, req CreatePaymentRequest) (*CreatePaymentResponse, error)
	// QueryOrder queries the payment status of the given trade number.
	QueryOrder(ctx context.Context, tradeNo string) (*QueryOrderResponse, error)
	// VerifyNotification parses and verifies a webhook callback.
	// Returns nil for unrecognized or irrelevant events (caller should return 200).
	VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*PaymentNotification, error)
	// Refund requests a refund from the upstream provider.
	Refund(ctx context.Context, req RefundRequest) (*RefundResponse, error)
}

// RefundQueryProvider extends Provider with refund status querying.
type RefundQueryProvider interface {
	Provider
	QueryRefund(ctx context.Context, req RefundQueryRequest) (*RefundResponse, error)
}

// CancelableProvider extends Provider with the ability to cancel pending payments.
type CancelableProvider interface {
	Provider
	// CancelPayment cancels/expires a pending payment on the upstream platform.
	CancelPayment(ctx context.Context, tradeNo string) error
}

// MerchantIdentityProvider exposes the current non-sensitive merchant identity
// derived from provider configuration for snapshot consistency checks.
type MerchantIdentityProvider interface {
	MerchantIdentityMetadata() map[string]string
}
