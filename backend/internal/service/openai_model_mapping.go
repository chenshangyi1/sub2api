package service

import "strings"

// resolveOpenAIForwardModel 解析 OpenAI 兼容转发使用的模型。
// messagesDispatchMappedModel 是调用方已为 /v1/messages 解析的显式调度结果；
// 普通 OpenAI 请求必须传空，避免将分组配置作为通用模型兜底。
func resolveOpenAIForwardModel(account *Account, requestedModel, messagesDispatchMappedModel string) string {
	messagesDispatchMappedModel = strings.TrimSpace(messagesDispatchMappedModel)
	if account == nil {
		if messagesDispatchMappedModel != "" {
			return messagesDispatchMappedModel
		}
		return requestedModel
	}

	mappedModel, matched := account.ResolveMappedModel(requestedModel)
	if !matched && messagesDispatchMappedModel != "" {
		return messagesDispatchMappedModel
	}
	return mappedModel
}

// openAIOAuthForeignModelPrefixes 列出明确属于其他厂商家族的模型名前缀。
// Codex 上游不可能服务这些模型：转发阶段 normalizeOpenAIModelForUpstream
// 对未知模型原样透传，上游必然返回不可重试的 400。
//
// 采用保守黑名单而非 Codex 模型白名单：未知/自定义别名保持「允许」，
// 以兼容渠道级模型映射等「账号选定之后才改写模型名」的部署方式
// （调度过滤看到的是改写前的原始模型名）。前缀分类的先例见
// ResolveThinkingProtocol（thinking_protocol.go）。
var openAIOAuthForeignModelPrefixes = []string{
	"deepseek-",
	"glm-",
	"kimi-",
	"moonshot-",
	"qwen-",
	"qwen2-",
	"qwen3-",
	"qwen4-",
	"qwq-",
	"minimax-",
	"gemini-",
	"gemma-",
	"grok-",
	"doubao-",
	"hunyuan-",
	"llama-",
	"llama2-",
	"llama3-",
	"meta-llama",
	"mistral-",
	"mixtral-",
	"baichuan-",
	"ernie-",
	"step-",
	"seed-",
	"yi-",
}

// isOpenAIOAuthServableModel 判断「空 model_mapping 的 OpenAI OAuth 账号」能否
// 服务请求模型。空映射默认仍是「允许」，仅排除明确属于其他厂商家族的模型
// （deepseek-*/glm-*、以及 Kimi Code 官方 bare ID k3 / k3-256k 等）——这类
// 请求原样透传必然被 Codex 上游以不可重试的 400 拒绝，且不触发 failover，
// 应在调度阶段就跳过该账号，把请求让给显式声明支持该模型的账号（#3662）。
// bare k3 仅精确匹配（取 last segment 后），不使用宽泛 k3- 前缀，以免误伤
// 自定义别名；显式 model_mapping 命中路径不经过本函数，语义不变。
func isOpenAIOAuthServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true // 空模型交由上层必填校验处理
	}
	// Kimi Code 官方 bare model ID：无厂商前缀，prefix 黑名单挡不住。
	if model == "k3" || model == "k3-256k" {
		return false
	}
	for _, prefix := range openAIOAuthForeignModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}
	return true
}

// resolveOpenAICompactForwardModel determines the compact-only upstream model
// for /responses/compact requests. It never affects normal /responses traffic.
// When no compact-specific mapping matches, the input model is returned as-is.
func resolveOpenAICompactForwardModel(account *Account, model string) string {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" || account == nil {
		return trimmedModel
	}

	mappedModel, matched := account.ResolveCompactMappedModel(trimmedModel)
	if !matched {
		return trimmedModel
	}
	if trimmedMapped := strings.TrimSpace(mappedModel); trimmedMapped != "" {
		return trimmedMapped
	}
	return trimmedModel
}

// isCNProviderServableModel 判断空 model_mapping 的国产供应商账号能否服务该模型。
// 空映射不再等于“允许所有”：zhipu 空 mapping 不能把 gpt-5.6-* 当成可服务，
// 否则复合组选号会先打国模、失败后再 failover，把 GPT 请求拖成容量不足 503。
// 入参是 cn_vendor（统一平台）或历史 platform 名（kimi/zhipu/deepseek）；
// custom 国模仍放行，显式 mapping / 透传不走这里。
func isCNProviderServableModel(platformOrVendor, requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(platformOrVendor)) {
	case PlatformZhipu:
		return hasAnyPrefix(model, "glm-", "cog", "chatglm")
	case PlatformKimi:
		return model == "k3" || model == "k3-256k" || hasAnyPrefix(model, "kimi-", "moonshot-", "k2-", "k1.5")
	case PlatformDeepseek:
		return isKnownDeepSeekModel(model)
	case CNVendorMiniMax:
		return hasAnyPrefix(model, "minimax-", "abab", "m2.")
	default:
		return true
	}
}

// isOpenAIAPIKeyEmptyMappingServableModel 限制空 mapping 的 OpenAI API Key
// 账号：只认自家 gpt-/o-/codex-/chatgpt-/sora-/dall-e 家族。空 mapping 不能
// 把 gemini-* / grok-* / deepseek-* 当成可服务，否则 Adaptive 混池会把请求
// 先打到「可达鸭 - bugpro」这类空映射账号，客户端收到 404。
func isOpenAIAPIKeyEmptyMappingServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	if !isOpenAIOAuthServableModel(requestedModel) {
		return false
	}
	return hasAnyPrefix(model, "gpt-", "o1", "o3", "o4", "codex-", "chatgpt-", "sora-", "dall-e", "gpt-image", "text-embedding", "whisper-", "tts-") ||
		model == "o1" || model == "o3" || model == "o4-mini"
}

func isGeminiEmptyMappingServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	return hasAnyPrefix(model, "gemini-", "gemma-", "imagen-", "veo-")
}

func isAnthropicEmptyMappingServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	return hasAnyPrefix(model, "claude-")
}

func isGrokEmptyMappingServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	return hasAnyPrefix(model, "grok-")
}

func hasAnyPrefix(model string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
