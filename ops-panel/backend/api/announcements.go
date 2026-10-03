package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerAnnouncements(g *gin.RouterGroup, d *Deps) {
	g.GET("/announcements", func(c *gin.Context) {
		if d.Announcements == nil {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{
				"items":     []any{},
				"total":     0,
				"page":      1,
				"page_size": 20,
				"pages":     1,
			}})
			return
		}

		page, pageSize, err := parsePageQuery(c)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		list, total, err := d.Announcements.ListPage(page, pageSize)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		pages := 1
		if total > 0 {
			pages = int((total + int64(pageSize) - 1) / int64(pageSize))
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"items":     list,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"pages":     pages,
		}})
	})
}
