# CN + Video Platform Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use test-driven-development. Do not commit unless the user asks.

**Goal:** Make account/group `platform` distinguish 国模 (`cn`) and 视频 (`video`) from OpenAI/Anthropic, keep adaptive protocol, and keep legacy `kimi`/`zhipu`/`deepseek` readable.

**Architecture:** `platform` is the scheduling identity. Vendor is a credential (`cn_vendor` / `video_vendor`). Legacy three CN platforms canonicalize to `cn` on write. Composite model detection maps Kimi/GLM/DeepSeek IDs to `cn`. OpenAI/Anthropic rows are never auto-migrated.

**Tech Stack:** Go 1.26 (`GOEXPERIMENT=jsonv2`), Vue 3 + Vitest, PostgreSQL SQL migration 254.

**Spec:** Session design — 国模合成一个平台、视频单独一类；厂商是预设；自适应协议复用现有 CN UI；Grok 视频仍走 Grok。

## Global Constraints

- Local unique baseline: `D:\CodexWorkspace\projects\sub2api-server`
- Do not deploy, migrate production, commit, or push unless the user confirms
- Do not auto-migrate existing OpenAI/Anthropic accounts
- Do not merge Grok Imagine into `video`
- Keep Gemini OpenAI-protocol methods
- TDD: failing test first for each behavior
- Skip smart-route and official home/login

### Task 1: Platform identity and vendor helpers
Write failing tests, then add PlatformCN / PlatformVideo, IsCNProvider("cn"), IsVideoProvider, CanonicalAccountPlatform, GetCNVendor.

### Task 2: OpenAI-compatible scheduling identity
DetectModelPlatform returns cn for kimi/glm/deepseek. NormalizeOpenAICompatiblePlatform keeps cn, video, grok, and legacy three. Scheduler buckets use cn + video.

### Task 3: Adaptive protocol on cn / video
Reuse existing adaptive protocol and base-url presets; vendor kimi/deepseek/custom keep native responses.

### Task 4: SQL migrate legacy three platforms only
254_unify_cn_video_platforms.sql updates accounts/groups kimi/zhipu/deepseek to cn. Do not touch openai/anthropic.

### Task 5: Frontend catalog and create/edit UI
Create modal second row: 国模 + 视频. Vendor preset then existing adaptive protocol UI.

### Task 6: Sweep remaining platform switch lists
Replace hardcoded kimi/zhipu/deepseek triples with IsCNProvider / cn+video.
