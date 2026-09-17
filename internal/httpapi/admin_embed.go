package httpapi

import (
	"embed"
	"net/http"
	"os"
)

//go:embed admin/index.html
var adminFiles embed.FS

func adminPage(c interface{ Data(int, string, []byte) }) {
	if data, err := os.ReadFile("admin/dist/index.html"); err == nil {
		c.Data(200, "text/html; charset=utf-8", data)
		return
	}
	data, err := adminFiles.ReadFile("admin/index.html")
	if err != nil {
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
