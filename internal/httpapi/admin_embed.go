package httpapi

import (
	"embed"
	"net/http"
)

//go:embed admin/index.html
var adminFiles embed.FS

func adminPage(c interface{ Data(int, string, []byte) }) {
	data, err := adminFiles.ReadFile("admin/index.html")
	if err != nil {
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
