package service

import "testing"

func TestIsCNProviderIncludesUnifiedAndLegacyPlatforms(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{PlatformCN, PlatformKimi, PlatformZhipu, PlatformDeepseek} {
		if !IsCNProvider(platform) {
			t.Fatalf("IsCNProvider(%q) = false, want true", platform)
		}
	}
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformVideo, PlatformGrok, ""} {
		if IsCNProvider(platform) {
			t.Fatalf("IsCNProvider(%q) = true, want false", platform)
		}
	}
}

func TestIsVideoProvider(t *testing.T) {
	t.Parallel()
	if !IsVideoProvider(PlatformVideo) {
		t.Fatal("IsVideoProvider(video) = false, want true")
	}
	if IsVideoProvider(PlatformCN) || IsVideoProvider(PlatformOpenAI) || IsVideoProvider(PlatformGrok) {
		t.Fatal("IsVideoProvider should only match video")
	}
}

func TestCanonicalAccountPlatformUnifiesLegacyCN(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		PlatformKimi:     PlatformCN,
		PlatformZhipu:    PlatformCN,
		PlatformDeepseek: PlatformCN,
		PlatformCN:       PlatformCN,
		PlatformVideo:    PlatformVideo,
		PlatformOpenAI:   PlatformOpenAI,
		PlatformGrok:     PlatformGrok,
	}
	for in, want := range cases {
		if got := CanonicalAccountPlatform(in); got != want {
			t.Fatalf("CanonicalAccountPlatform(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetCNVendorFromUnifiedAndLegacyAccounts(t *testing.T) {
	t.Parallel()
	unified := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": "kimi"}}
	if got := unified.GetCNVendor(); got != CNVendorKimi {
		t.Fatalf("unified vendor = %q, want %q", got, CNVendorKimi)
	}
	legacy := &Account{Platform: PlatformDeepseek}
	if got := legacy.GetCNVendor(); got != CNVendorDeepseek {
		t.Fatalf("legacy vendor = %q, want %q", got, CNVendorDeepseek)
	}
	custom := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": "custom"}}
	if got := custom.GetCNVendor(); got != CNVendorCustom {
		t.Fatalf("custom vendor = %q, want %q", got, CNVendorCustom)
	}
	minimax := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": "minimax"}}
	if got := minimax.GetCNVendor(); got != CNVendorMiniMax {
		t.Fatalf("minimax vendor = %q, want %q", got, CNVendorMiniMax)
	}
}

func TestGetVideoVendor(t *testing.T) {
	t.Parallel()
	account := &Account{Platform: PlatformVideo, Credentials: map[string]any{"video_vendor": "sora"}}
	if got := account.GetVideoVendor(); got != VideoVendorSora {
		t.Fatalf("video vendor = %q, want %q", got, VideoVendorSora)
	}
	if got := (&Account{Platform: PlatformOpenAI}).GetVideoVendor(); got != "" {
		t.Fatalf("non-video vendor = %q, want empty", got)
	}
}

func TestUnifiedCNAccountUsesVendorForProtocolAndBaseURL(t *testing.T) {
	t.Parallel()

	kimi := &Account{
		Platform: PlatformCN,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"cn_vendor":    CNVendorKimi,
			"account_mode": AccountModePayG,
			"api_protocol": APIProtocolAdaptive,
		},
	}
	if !kimi.SupportsNativeCNResponses() {
		t.Fatal("cn+kimi should support native responses")
	}
	if kimi.GetAPIProtocol() != APIProtocolAdaptive {
		t.Fatalf("protocol = %q, want adaptive", kimi.GetAPIProtocol())
	}
	if got := kimi.GetOpenAIBaseURL(); got != DefaultKimiPayGBaseURL {
		t.Fatalf("kimi base url = %q, want %q", got, DefaultKimiPayGBaseURL)
	}

	zhipu := &Account{
		Platform:    PlatformCN,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"cn_vendor": CNVendorZhipu, "api_protocol": APIProtocolResponses},
	}
	if zhipu.SupportsNativeCNResponses() {
		t.Fatal("cn+zhipu should not support native responses")
	}
	if zhipu.GetAPIProtocol() != APIProtocolChatCompletions {
		t.Fatalf("zhipu responses protocol = %q, want chat_completions fallback", zhipu.GetAPIProtocol())
	}

	video := &Account{
		Platform: PlatformVideo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"video_vendor": VideoVendorSora,
			"api_protocol": APIProtocolAdaptive,
			"base_url":     "https://video.example/v1",
		},
	}
	if !video.IsOpenAICompatible() {
		t.Fatal("video should be OpenAI-compatible")
	}
	if video.GetAPIProtocol() != APIProtocolAdaptive {
		t.Fatalf("video protocol = %q, want adaptive", video.GetAPIProtocol())
	}
	if got := video.GetOpenAIBaseURL(); got != "https://video.example/v1" {
		t.Fatalf("video base url = %q", got)
	}

	minimax := &Account{
		Platform: PlatformCN,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"cn_vendor":    CNVendorMiniMax,
			"account_mode": AccountModePayG,
			"api_protocol": APIProtocolAdaptive,
		},
	}
	if !minimax.SupportsNativeCNResponses() {
		t.Fatal("cn+minimax should support native responses")
	}
	if got := minimax.GetOpenAIBaseURL(); got != DefaultMiniMaxBaseURL {
		t.Fatalf("minimax chat url = %q, want %q", got, DefaultMiniMaxBaseURL)
	}
	if got := minimax.GetCNProtocolBaseURL(APIProtocolAnthropic); got != DefaultMiniMaxAnthropicBaseURL {
		t.Fatalf("minimax anthropic url = %q, want %q", got, DefaultMiniMaxAnthropicBaseURL)
	}
	if got := minimax.GetCNProtocolBaseURL(APIProtocolResponses); got != DefaultMiniMaxBaseURL {
		t.Fatalf("minimax responses url = %q, want %q", got, DefaultMiniMaxBaseURL)
	}
	coding := &Account{
		Platform: PlatformCN,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"cn_vendor":    CNVendorMiniMax,
			"account_mode": AccountModeCoding,
			"api_protocol": APIProtocolAdaptive,
		},
	}
	if got := coding.GetOpenAIBaseURL(); got != DefaultMiniMaxBaseURL {
		t.Fatalf("minimax coding still uses the shared inference host: %q", got)
	}
}

func TestPrepareAccountPlatformWriteFoldsLegacyCNAndSeedsVendor(t *testing.T) {
	t.Parallel()
	platform, creds := PrepareAccountPlatformWrite(PlatformKimi, map[string]any{"api_key": "sk"})
	if platform != PlatformCN {
		t.Fatalf("platform = %q, want cn", platform)
	}
	if got := creds["cn_vendor"]; got != CNVendorKimi {
		t.Fatalf("cn_vendor = %v, want %q", got, CNVendorKimi)
	}
	platform, creds = PrepareAccountPlatformWrite(PlatformCN, map[string]any{"cn_vendor": CNVendorZhipu})
	if platform != PlatformCN {
		t.Fatalf("unified platform = %q, want cn", platform)
	}
	if got := creds["cn_vendor"]; got != CNVendorZhipu {
		t.Fatalf("existing vendor overwritten: %v", got)
	}
	platform, creds = PrepareAccountPlatformWrite(PlatformVideo, map[string]any{"video_vendor": VideoVendorSora})
	if platform != PlatformVideo {
		t.Fatalf("video platform = %q", platform)
	}
	if got := creds["video_vendor"]; got != VideoVendorSora {
		t.Fatalf("video vendor = %v", got)
	}
	platform, creds = PrepareAccountPlatformWrite(PlatformOpenAI, map[string]any{"api_key": "sk"})
	if platform != PlatformOpenAI {
		t.Fatalf("openai folded: %q", platform)
	}
}

func TestAccountMatchesPlatformFoldsLegacyCN(t *testing.T) {
	t.Parallel()
	if !AccountMatchesPlatform(PlatformKimi, PlatformCN) {
		t.Fatal("kimi account must match cn request")
	}
	if !AccountMatchesPlatform(PlatformCN, PlatformKimi) {
		t.Fatal("cn account must match legacy kimi request")
	}
	if !AccountMatchesPlatform(PlatformDeepseek, PlatformCN) {
		t.Fatal("deepseek account must match cn request")
	}
	if AccountMatchesPlatform(PlatformZhipu, PlatformKimi) {
		t.Fatal("legacy zhipu account must not match kimi request")
	}
	if AccountMatchesPlatform(PlatformOpenAI, PlatformCN) {
		t.Fatal("openai account must not match cn")
	}
	if AccountMatchesPlatform(PlatformVideo, PlatformCN) {
		t.Fatal("video account must not match cn")
	}
}

func TestAccountMatchesRequestedPlatformUsesVendor(t *testing.T) {
	t.Parallel()
	kimiAcc := &Account{Platform: PlatformKimi}
	if !kimiAcc.MatchesRequestedPlatform(PlatformCN) {
		t.Fatal("legacy kimi account must serve cn groups")
	}
	cnKimi := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": CNVendorKimi}}
	if !cnKimi.MatchesRequestedPlatform(PlatformKimi) {
		t.Fatal("cn+kimi vendor must serve leftover kimi groups")
	}
	cnZhipu := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": CNVendorZhipu}}
	if cnZhipu.MatchesRequestedPlatform(PlatformKimi) {
		t.Fatal("cn+zhipu vendor must not serve leftover kimi groups")
	}
	cnCustom := &Account{Platform: PlatformCN, Credentials: map[string]any{"cn_vendor": CNVendorCustom}}
	if cnCustom.MatchesRequestedPlatform(PlatformKimi) {
		t.Fatal("cn+custom vendor must not serve leftover kimi groups")
	}
	if !cnCustom.MatchesRequestedPlatform(PlatformCN) {
		t.Fatal("cn+custom vendor must serve cn groups")
	}
}

func TestExpandSchedulablePlatformsIncludesLegacyCN(t *testing.T) {
	t.Parallel()
	got := ExpandSchedulablePlatforms(PlatformCN)
	want := map[string]struct{}{PlatformCN: {}, PlatformKimi: {}, PlatformZhipu: {}, PlatformDeepseek: {}}
	if len(got) != len(want) {
		t.Fatalf("expand cn len=%d want %d: %v", len(got), len(want), got)
	}
	for _, platform := range got {
		if _, ok := want[platform]; !ok {
			t.Fatalf("unexpected expanded platform %q", platform)
		}
	}
	kimi := ExpandSchedulablePlatforms(PlatformKimi)
	if len(kimi) != 2 {
		t.Fatalf("expand kimi = %v, want kimi+cn", kimi)
	}
	seen := map[string]struct{}{}
	for _, platform := range kimi {
		seen[platform] = struct{}{}
	}
	if _, ok := seen[PlatformKimi]; !ok {
		t.Fatalf("expand kimi missing kimi: %v", kimi)
	}
	if _, ok := seen[PlatformCN]; !ok {
		t.Fatalf("expand kimi missing cn: %v", kimi)
	}
	if got := ExpandSchedulablePlatforms(PlatformOpenAI); len(got) != 1 || got[0] != PlatformOpenAI {
		t.Fatalf("expand openai = %v", got)
	}
}

func TestNormalizeGroupPlatformFoldsLegacyCN(t *testing.T) {
	t.Parallel()
	if got := NormalizeGroupPlatform(PlatformKimi); got != PlatformCN {
		t.Fatalf("NormalizeGroupPlatform(kimi) = %q, want cn", got)
	}
	if got := NormalizeGroupPlatform(""); got != PlatformAnthropic {
		t.Fatalf("empty group platform = %q, want anthropic", got)
	}
	if got := NormalizeGroupPlatform(PlatformVideo); got != PlatformVideo {
		t.Fatalf("video group platform = %q", got)
	}
}
