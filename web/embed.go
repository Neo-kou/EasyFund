// Package web 前端静态资源, go:embed 打进二进制, 部署产物仍是单文件。
package web

import "embed"

//go:embed index.html app.js style.css
var FS embed.FS
