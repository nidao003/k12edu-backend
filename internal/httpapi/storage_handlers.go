package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/nidao003/k12edu-backend/internal/storage"
	"net/http"
)

type storageHandler struct{ store storage.Store }

func (h storageHandler) Upload(c *gin.Context) {
	f, err := c.FormFile("file")
	if err != nil || f.Size > 25<<20 {
		Error(c, 400, "FILE_INVALID", "file is required and must be <=25MB")
		return
	}
	src, err := f.Open()
	if err != nil {
		Error(c, 400, "FILE_INVALID", "cannot open file")
		return
	}
	defer src.Close()
	key, err := h.store.Put(c, f.Filename, src)
	if err != nil {
		Error(c, 500, "FILE_SAVE_FAILED", "cannot save file")
		return
	}
	c.JSON(201, gin.H{"key": key, "name": f.Filename, "size": f.Size})
}
func (h storageHandler) Download(c *gin.Context) {
	r, err := h.store.Open(c, c.Param("key"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer r.Close()
	c.DataFromReader(200, -1, "application/octet-stream", r, nil)
}
