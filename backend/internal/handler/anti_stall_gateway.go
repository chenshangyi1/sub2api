package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// installAntiStallForKey enables hold-back + drip when admin module is on and
// the API key opted into a non-off tier. Works for OpenAI / Anthropic / Gemini
// gateway writers (SSE or chunked).
func installAntiStallForKey(
	ctx context.Context,
	c *gin.Context,
	settings *service.SettingService,
	apiKey *service.APIKey,
	reqLog *zap.Logger,
	logTag string,
) *service.AntiStallSession {
	if c == nil || settings == nil || apiKey == nil {
		return nil
	}
	if existing := service.AntiStallSessionFromGin(c); existing != nil {
		return existing
	}
	adminCfg := settings.GetAntiStallAdminConfig(ctx)
	cfg := service.ResolveAntiStallForKey(adminCfg, apiKey.AntiStallTier)
	if !cfg.Enabled {
		return nil
	}
	session := service.NewAntiStallSession(cfg)
	_ = service.InstallAntiStallProWriter(c, session)
	if reqLog != nil {
		if logTag == "" {
			logTag = "anti_stall_pro"
		}
		reqLog.Info(logTag+".enabled",
			zap.String("tier", cfg.Tier),
			zap.Int("buffer_tokens", cfg.BufferTokens),
			zap.Int("drip_per_sec", cfg.DripTokensPerSecond),
			zap.Int("upstream_max_retry", cfg.UpstreamMaxRetry),
			zap.Int("max_drip_seconds", cfg.MaxDripSeconds),
			zap.Int("max_leaf_switches", cfg.MaxLeafSwitches),
		)
	}
	return session
}

// antiStallOnUpstreamFailure enters drip/recovery. Returns true if the caller
// should still try to failover (another account/leaf); false if fail-hard.
func antiStallOnUpstreamFailure(c *gin.Context, failoverErr *service.UpstreamFailoverError) (mayFailover bool) {
	anti := service.AntiStallSessionFromGin(c)
	if anti == nil || !anti.Config().Enabled {
		return true // no anti-stall: normal failover rules apply
	}
	anti.BeginRecovery()
	service.EnsureAntiStallDripRunning(c, anti)
	if anti.ShouldFailHard() {
		return false
	}
	return true
}

// antiStallForceSwitchRecommended uses platform error tables / status to decide
// whether same-leaf retries are unlikely to help.
func antiStallForceSwitchRecommended(failoverErr *service.UpstreamFailoverError) bool {
	if failoverErr == nil {
		return false
	}
	if service.AntiStallForceLeafFromUpstream(failoverErr.StatusCode, failoverErr.ResponseBody) {
		return true
	}
	return antiStallForceLeafSwitch(failoverErr)
}

// antiStallAllowFailoverAfterWrite: when hold-back is active, mid-stream
// recoverable errors can still switch if SafeToFailoverAfterWrite or force-leaf policy.
func antiStallAllowFailoverAfterWrite(c *gin.Context, failoverErr *service.UpstreamFailoverError) bool {
	anti := service.AntiStallSessionFromGin(c)
	if anti == nil || !anti.Config().Enabled {
		return false
	}
	if failoverErr != nil && failoverErr.SafeToFailoverAfterWrite {
		return true
	}
	return antiStallForceSwitchRecommended(failoverErr)
}

// antiStallEndSuccess clears drip after a healthy complete response.
func antiStallEndSuccess(c *gin.Context) {
	if anti := service.AntiStallSessionFromGin(c); anti != nil {
		anti.EndRecovery()
		service.StopAntiStallDrip(c)
	}
}
