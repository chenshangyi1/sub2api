package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bejix/upstream-ops/backend/operations"
	"github.com/gin-gonic/gin"
)

func registerOperations(api *gin.RouterGroup, d *Deps) {
	if d.Operations == nil {
		return
	}
	root := api.Group("/operations")
	root.GET("/targets", func(c *gin.Context) {
		result, err := d.Operations.ListTargets(c.Request.Context())
		operationsResult(c, result, err)
	})
	root.GET("/diagnostics", func(c *gin.Context) {
		result, err := d.Operations.GetDiagnostics(c.Request.Context())
		operationsResult(c, result, err)
	})
	root.POST("/diagnostics/cleanup", func(c *gin.Context) {
		result, err := d.Operations.CleanupInvalidData(c.Request.Context())
		operationsResult(c, result, err)
	})
	root.GET("/analytics", func(c *gin.Context) {
		result, err := d.Operations.GetAnalytics(c.Request.Context(), c.DefaultQuery("range", "day"))
		operationsResult(c, result, err)
	})
	root.GET("/group-monitor", func(c *gin.Context) {
		result, err := d.Operations.GetGroupMonitorOverview(c.Request.Context())
		operationsResult(c, result, err)
	})
	root.GET("/settings", func(c *gin.Context) {
		result, err := d.Operations.GetSettings(c.Request.Context())
		operationsResult(c, result, err)
	})
	root.PUT("/settings", func(c *gin.Context) {
		var input operations.OperationsSettings
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.SaveSettings(c.Request.Context(), input)
		operationsResult(c, result, err)
	})

	target := root.Group("/targets/:targetID")
	target.GET("/groups", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.ListTargetGroups(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.POST("/groups", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.CreateTargetGroupInput
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.CreateTargetGroup(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.GET("/accounts", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.ListAccounts(c.Request.Context(), targetID, operations.AccountFilter{
			Page: operationsQueryInt(c, "page", 1), PageSize: operationsQueryInt(c, "page_size", 50),
			Search: strings.TrimSpace(c.Query("search")), Schedule: c.DefaultQuery("schedule", "all"),
		})
		operationsResult(c, result, err)
	})
	target.POST("/accounts/import-api-key", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.ImportAPIKeyInput
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.ImportAPIKey(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.POST("/accounts/:accountID/actions", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		accountID, ok := operationsInt64Param(c, "accountID")
		if !ok {
			return
		}
		var input struct {
			Action string `json:"action" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		err := d.Operations.RunAccountAction(c.Request.Context(), targetID, accountID, input.Action)
		operationsResult(c, gin.H{"ok": err == nil}, err)
	})
	target.DELETE("/accounts/:accountID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		accountID, ok := operationsInt64Param(c, "accountID")
		if !ok {
			return
		}
		err := d.Operations.DeleteAccount(c.Request.Context(), targetID, accountID)
		operationsResult(c, gin.H{"ok": err == nil}, err)
	})

	target.GET("/probes", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.ListProbes(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.POST("/probes", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.CreateProbeInput
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.CreateProbe(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.POST("/probes/:probeID/run", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		probeID, ok := operationsInt64Param(c, "probeID")
		if !ok {
			return
		}
		result, err := d.Operations.RunProbe(c.Request.Context(), targetID, probeID)
		operationsResult(c, result, err)
	})
	target.PUT("/probes/:probeID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		probeID, ok := operationsInt64Param(c, "probeID")
		if !ok {
			return
		}
		var input struct {
			Enabled *bool `json:"enabled" binding:"required"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
			if err == nil {
				err = operations.ErrInvalid
			}
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.SetProbeEnabled(c.Request.Context(), targetID, probeID, *input.Enabled)
		operationsResult(c, result, err)
	})
	target.DELETE("/probes/:probeID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		probeID, ok := operationsInt64Param(c, "probeID")
		if !ok {
			return
		}
		err := d.Operations.DeleteProbe(c.Request.Context(), targetID, probeID)
		operationsResult(c, gin.H{"ok": err == nil}, err)
	})
	target.POST("/probes/run", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input struct {
			Mode       string  `json:"mode" binding:"required"`
			ProbeIDs   []int64 `json:"probe_ids"`
			AccountIDs []int64 `json:"account_ids"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		queued, err := d.Operations.RunProbeBatch(c.Request.Context(), targetID, input.Mode, operations.ProbeBatchFilter{
			ProbeIDs: input.ProbeIDs, AccountIDs: input.AccountIDs,
		})
		operationsResult(c, gin.H{"queued": queued}, err)
	})

	target.GET("/settings", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.GetTargetSettings(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.PUT("/settings", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.TargetSettings
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.SaveTargetSettings(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.POST("/settings/test-balance-webhook", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		err := d.Operations.TestBalanceWebhook(c.Request.Context(), targetID)
		operationsResult(c, gin.H{"ok": err == nil}, err)
	})

	target.GET("/automation", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.GetAutomationSettings(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.PUT("/automation", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.AutomationSettings
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.SaveAutomationSettings(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.POST("/automation/apply", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.ApplyAutomation(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})

	root.GET("/pool-execution", func(c *gin.Context) {
		mode, err := d.Operations.GetPoolExecutionMode(c.Request.Context())
		operationsResult(c, gin.H{"mode": mode}, err)
	})
	root.PUT("/pool-execution", func(c *gin.Context) {
		var input struct {
			Mode string `json:"mode"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		mode, err := d.Operations.SetPoolExecutionMode(c.Request.Context(), input.Mode)
		operationsResult(c, gin.H{"mode": mode}, err)
	})

	target.GET("/rate-policies", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.GetRatePolicies(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.PUT("/rate-policies/:targetType/:targetObjectID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		objectID, ok := operationsInt64Param(c, "targetObjectID")
		if !ok {
			return
		}
		var input operations.RatePolicyInput
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.SaveRatePolicy(c.Request.Context(), targetID, c.Param("targetType"), objectID, input)
		operationsResult(c, result, err)
	})
	target.POST("/rate-policies/apply", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.QueueRatePolicies(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})

	target.GET("/announcement-rules", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		result, err := d.Operations.ListAnnouncementRules(c.Request.Context(), targetID)
		operationsResult(c, result, err)
	})
	target.POST("/announcement-rules", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		var input operations.AnnouncementRule
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.CreateAnnouncementRule(c.Request.Context(), targetID, input)
		operationsResult(c, result, err)
	})
	target.PUT("/announcement-rules/:ruleID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		ruleID, ok := operationsInt64Param(c, "ruleID")
		if !ok {
			return
		}
		var input operations.AnnouncementRule
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := d.Operations.UpdateAnnouncementRule(c.Request.Context(), targetID, ruleID, input)
		operationsResult(c, result, err)
	})
	target.DELETE("/announcement-rules/:ruleID", func(c *gin.Context) {
		targetID, ok := operationsTargetID(c)
		if !ok {
			return
		}
		ruleID, ok := operationsInt64Param(c, "ruleID")
		if !ok {
			return
		}
		err := d.Operations.DeleteAnnouncementRule(c.Request.Context(), targetID, ruleID)
		operationsResult(c, gin.H{"ok": err == nil}, err)
	})
}

func operationsResult(c *gin.Context, value any, err error) {
	if err != nil {
		fail(c, operations.ErrorStatus(err), err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func operationsTargetID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("targetID"), 10, 32)
	if err != nil || value == 0 {
		fail(c, http.StatusBadRequest, operations.ErrInvalid)
		return 0, false
	}
	return uint(value), true
}

func operationsInt64Param(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 {
		fail(c, http.StatusBadRequest, operations.ErrInvalid)
		return 0, false
	}
	return value, true
}

func operationsQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}
