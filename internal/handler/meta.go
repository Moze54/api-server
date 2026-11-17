// 已注册能力的清单，方便 apiHub 配置渠道时发现可用接口。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"api-server/internal/provider"
	"api-server/internal/scraper"
)

type capability struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// Meta 列出全部已注册的爬虫与厂商适配器（任意有效 key 可访问）。
func Meta(scraperReg *scraper.Registry, vendorReg *provider.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		spiders := make([]capability, 0)
		for _, s := range scraperReg.List() {
			spiders = append(spiders, capability{Name: s.Name(), Desc: s.Desc()})
		}
		vendors := make([]capability, 0)
		for _, v := range vendorReg.List() {
			vendors = append(vendors, capability{Name: v.Name(), Desc: v.Desc()})
		}
		c.JSON(http.StatusOK, gin.H{"spiders": spiders, "vendors": vendors})
	}
}
