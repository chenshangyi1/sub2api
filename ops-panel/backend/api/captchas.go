package api

import (
	"net/http"

	"github.com/bejix/upstream-ops/backend/captcha"
	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func registerCaptchas(g *gin.RouterGroup, d *Deps) {
	gp := g.Group("/captcha-configs")
	gp.GET("", func(c *gin.Context) {
		list, err := d.Captchas.List()
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	})
	gp.POST("", func(c *gin.Context) { createCaptcha(c, d) })
	gp.PUT("/:id", func(c *gin.Context) { updateCaptcha(c, d) })
	gp.POST("/:id/refresh-balance", func(c *gin.Context) { refreshCaptchaBalance(c, d) })
	gp.DELETE("/:id", func(c *gin.Context) {
		id, err := uintParam(c, "id")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := d.Captchas.Delete(id); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}

type captchaInput struct {
	Name         string                      `json:"name" binding:"required"`
	Type         storage.CaptchaProviderType `json:"type" binding:"required"`
	APIKey       string                      `json:"api_key"`
	Endpoint     string                      `json:"endpoint"`
	Extra        string                      `json:"extra"`
	Enabled      bool                        `json:"enabled"`
	ProxyEnabled bool                        `json:"proxy_enabled"`
}

func createCaptcha(c *gin.Context, d *Deps) {
	var in captchaInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	key, err := d.Cipher.Encrypt(in.APIKey)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	cfg := &storage.CaptchaConfig{
		Name:         in.Name,
		Type:         in.Type,
		APIKeyCipher: key,
		Endpoint:     in.Endpoint,
		Extra:        in.Extra,
		Enabled:      in.Enabled,
		ProxyEnabled: in.ProxyEnabled,
	}
	if err := d.Captchas.Create(cfg); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}

func refreshCaptchaBalance(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	cfg, err := d.Captchas.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	proxyCfg := config.ProxyConfig{}
	if d.Runtime != nil {
		proxyCfg = d.Runtime.CurrentProxy()
	}
	updated, err := captcha.RefreshBalanceWithProxy(c.Request.Context(), d.Captchas, d.Cipher, cfg, proxyCfg)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func updateCaptcha(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	cfg, err := d.Captchas.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	var in captchaInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	cfg.Name = in.Name
	cfg.Type = in.Type
	cfg.Endpoint = in.Endpoint
	cfg.Extra = in.Extra
	cfg.Enabled = in.Enabled
	cfg.ProxyEnabled = in.ProxyEnabled
	if in.APIKey != "" {
		key, err := d.Cipher.Encrypt(in.APIKey)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		cfg.APIKeyCipher = key
	}
	if err := d.Captchas.Update(cfg); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}
