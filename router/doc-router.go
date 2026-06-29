package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed doc/seedance-video-generation.md doc/seedance-asset-management.md
var docFS embed.FS

// docViewerHTML 是一个自包含的 Markdown 查看器：用 marked.js 在浏览器端渲染，
// 不引入服务端 markdown 依赖。占位符 __TITLE__ / __MDPATH__ 运行时替换。
const docViewerHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>__TITLE__</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/github-markdown-css@5/github-markdown-light.min.css">
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<style>
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;background:#fff;}
.nav{position:sticky;top:0;height:48px;background:#24292f;color:#fff;display:flex;align-items:center;padding:0 20px;gap:18px;z-index:10;}
.nav strong{font-size:14px;}
.nav a{color:#fff;text-decoration:none;opacity:.8;font-size:14px;}
.nav a:hover{opacity:1;}
.markdown-body{box-sizing:border-box;max-width:980px;margin:24px auto 60px;padding:0 24px;}
@media(max-width:767px){.markdown-body{padding:0 16px;}}
.markdown-body pre{overflow:auto;}
</style>
</head>
<body>
<div class="nav">
  <strong>API 文档</strong>
  <a href="/doc/seedance-video">视频生成</a>
  <a href="/doc/seedance-asset">素材库管理</a>
</div>
<article id="content" class="markdown-body">加载中…</article>
<script>
fetch("__MDPATH__").then(function(r){return r.text();}).then(function(t){
  document.getElementById("content").innerHTML = marked.parse(t);
}).catch(function(e){document.getElementById("content").textContent="文档加载失败: "+e;});
</script>
</body>
</html>`

const docIndexHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API 文档</title>
<style>
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;background:#f6f8fa;color:#24292f;}
.wrap{max-width:760px;margin:80px auto;padding:0 24px;}
h1{font-size:26px;}
.card{display:block;background:#fff;border:1px solid #d0d7de;border-radius:10px;padding:20px 22px;margin:16px 0;text-decoration:none;color:#24292f;transition:box-shadow .15s;}
.card:hover{box-shadow:0 3px 12px rgba(0,0,0,.08);}
.card h2{margin:0 0 6px;font-size:18px;color:#0969da;}
.card p{margin:0;color:#57606a;font-size:14px;}
</style>
</head>
<body>
<div class="wrap">
<h1>Seedance API 文档</h1>
<a class="card" href="/doc/seedance-video"><h2>Seedance 2.0 视频生成 API →</h2><p>文生/图生/首尾帧/参考/多模态视频生成，提交与查询任务、计费、错误码。</p></a>
<a class="card" href="/doc/seedance-asset"><h2>Seedance 素材库管理 API →</h2><p>创建素材组、上传素材、查询状态，并以 asset:// 在视频生成中引用。免费。</p></a>
</div>
</body>
</html>`

// SetDocRouter 注册公开的 API 文档路由（/doc，无需登录鉴权）。
func SetDocRouter(router *gin.Engine) {
	doc := router.Group("/doc")
	{
		doc.GET("", docIndex)
		doc.GET("/", docIndex)
		doc.GET("/seedance-video", docViewer("Seedance 2.0 视频生成 API", "/doc/seedance-video.md"))
		doc.GET("/seedance-asset", docViewer("Seedance 素材库管理 API", "/doc/seedance-asset.md"))
		doc.GET("/seedance-video.md", docRaw("doc/seedance-video-generation.md"))
		doc.GET("/seedance-asset.md", docRaw("doc/seedance-asset-management.md"))
	}
}

func docIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docIndexHTML))
}

func docViewer(title, mdPath string) gin.HandlerFunc {
	html := strings.NewReplacer("__TITLE__", title, "__MDPATH__", mdPath).Replace(docViewerHTML)
	body := []byte(html)
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", body)
	}
}

func docRaw(embedPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		b, err := docFS.ReadFile(embedPath)
		if err != nil {
			c.Data(http.StatusNotFound, "text/plain; charset=utf-8", []byte("not found"))
			return
		}
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", b)
	}
}
