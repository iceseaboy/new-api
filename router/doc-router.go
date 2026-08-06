package router

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed doc/seedance-video-generation.md doc/seedance-asset-management.md doc/kling-video-generation.md doc/happyhorse-video-generation.md
var docFS embed.FS

// SetDocRouter 注册公开的 API 文档内容接口（无需登录鉴权）。
//
// 文档页面本身由前端 SPA 在 /docs 路由内渲染，复用平台布局与样式；
// 这里只提供 markdown 原文接口供前端拉取。
// 返回 {success, data} 格式，与平台其它文档接口（user-agreement 等）一致。
func SetDocRouter(router *gin.Engine) {
	doc := router.Group("/api/doc")
	{
		doc.GET("/seedance-video", docContent("doc/seedance-video-generation.md"))
		doc.GET("/seedance-asset", docContent("doc/seedance-asset-management.md"))
		doc.GET("/kling-video", docContent("doc/kling-video-generation.md"))
		doc.GET("/happyhorse-video", docContent("doc/happyhorse-video-generation.md"))
	}
	// 旧版文档页地址 /doc 兼容跳转（classic 时代的入口）
	router.GET("/doc", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/docs")
	})
}

func docContent(embedPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		b, err := docFS.ReadFile(embedPath)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "文档不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": string(b)})
	}
}
