package service

import "testing"

func TestIsCNProviderServableModel_EmptyMappingFamilies(t *testing.T) {
	t.Parallel()

	if isCNProviderServableModel(PlatformZhipu, "gpt-5.6") {
		t.Fatal("zhipu empty mapping must not admit gpt-5.6")
	}
	if !isCNProviderServableModel(PlatformZhipu, "glm-4.6") {
		t.Fatal("zhipu empty mapping must admit glm-4.6")
	}
	if isCNProviderServableModel(PlatformKimi, "gpt-5.6") {
		t.Fatal("kimi empty mapping must not admit gpt-5.6")
	}
	if !isCNProviderServableModel(PlatformKimi, "kimi-k2.5") {
		t.Fatal("kimi empty mapping must admit kimi-k2.5")
	}
	if isCNProviderServableModel(PlatformDeepseek, "gpt-5.6") {
		t.Fatal("deepseek empty mapping must not admit gpt-5.6")
	}
	if !isCNProviderServableModel(PlatformDeepseek, "deepseek-chat") {
		t.Fatal("deepseek empty mapping must admit deepseek-chat")
	}
	if !isCNProviderServableModel(PlatformDeepseek, "deepseek-v3.2") {
		t.Fatal("deepseek empty mapping must admit known deepseek-v3.2")
	}
	if isCNProviderServableModel(PlatformDeepseek, "deepseek-v9-unknown") {
		t.Fatal("deepseek empty mapping must reject unknown models")
	}
	if isCNProviderServableModel(CNVendorMiniMax, "gpt-5.6") {
		t.Fatal("minimax empty mapping must not admit gpt-5.6")
	}
	if !isCNProviderServableModel(CNVendorMiniMax, "minimax-m2") {
		t.Fatal("minimax empty mapping must admit minimax-m2")
	}
}

func TestZhipuEmptyMappingIsModelSupported(t *testing.T) {
	t.Parallel()
	account := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	if account.IsModelSupported("gpt-5.6") {
		t.Fatal("zhipu account with empty mapping must not support gpt-5.6")
	}
	if !account.IsModelSupported("glm-4.5") {
		t.Fatal("zhipu account with empty mapping must support glm-4.5")
	}
}

func TestCNVendorEmptyMappingIsModelSupported(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform:    PlatformCN,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"cn_vendor": CNVendorZhipu},
	}
	if account.IsModelSupported("gpt-5.6") {
		t.Fatal("unified cn zhipu empty mapping must not support gpt-5.6")
	}
	if !account.IsModelSupported("glm-4.5") {
		t.Fatal("unified cn zhipu empty mapping must support glm-4.5")
	}
}

func TestEmptyMappingFamilyGates(t *testing.T) {
	t.Parallel()

	openai := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	if openai.IsModelSupported("gemini-3.8-flash") || openai.IsModelSupported("deepseek-v4") || openai.IsModelSupported("grok-4") {
		t.Fatal("openai empty mapping must not admit foreign families")
	}
	if !openai.IsModelSupported("gpt-6-astra") || !openai.IsModelSupported("gpt-5.3-codex") {
		t.Fatal("openai empty mapping must admit gpt-/codex- family")
	}

	gemini := &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	if gemini.IsModelSupported("gpt-5.4") || !gemini.IsModelSupported("gemini-3.8-flash") {
		t.Fatal("gemini empty mapping must only admit gemini family")
	}

	anthropic := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	if anthropic.IsModelSupported("gpt-5.4") || !anthropic.IsModelSupported("claude-sonnet-4") {
		t.Fatal("anthropic empty mapping must only admit claude family")
	}

	if isGrokEmptyMappingServableModel("gpt-5.4") || !isGrokEmptyMappingServableModel("grok-4") {
		t.Fatal("grok empty mapping helper must only admit grok family")
	}
}

func TestAccountsSupportingRequestedModel_DropsForeignEmptyMapping(t *testing.T) {
	t.Parallel()
	accounts := []Account{
		{ID: 1, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{}},
	}
	got := accountsSupportingRequestedModel(accounts, "gpt-5.6")
	if len(got) != 0 {
		t.Fatalf("expected no zhipu empty-mapping accounts for gpt-5.6, got %d", len(got))
	}
	filtered, unsupported := filterAccountsSupportingRequestedModel(accounts, "gpt-5.6")
	if len(filtered) != 0 || unsupported != 1 {
		t.Fatalf("filtered=%d unsupported=%d, want 0/1", len(filtered), unsupported)
	}
}
