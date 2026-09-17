package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nidao003/k12edu-backend/internal/storage"
	"net/http"
)

type storageHandler struct {
	store storage.Store
	db    *pgxpool.Pool
}

func (h storageHandler) Upload(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	f, err := c.FormFile("file")
	if e != nil || err != nil || f.Size > 25<<20 {
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
	id := uuid.New()
	if _, err = h.db.Exec(c, `INSERT INTO user_files(id,user_id,storage_key,name,size_bytes,content_type) VALUES($1,$2,$3,$4,$5,$6)`, id, uid, key, f.Filename, f.Size, f.Header.Get("Content-Type")); err != nil {
		_ = h.store.Delete(c, key)
		Error(c, 500, "FILE_METADATA_FAILED", "cannot save file metadata")
		return
	}
	c.JSON(201, gin.H{"id": id, "key": key, "name": f.Filename, "size": f.Size})
}
func (h storageHandler) List(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.Status(401)
		return
	}
	rows, e := h.db.Query(c, `SELECT id,name,size_bytes,content_type,created_at FROM user_files WHERE user_id=$1 ORDER BY created_at DESC LIMIT 200`, uid)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, name, typ string
		var size int64
		var created any
		if e := rows.Scan(&id, &name, &size, &typ, &created); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "name": name, "size": size, "contentType": typ, "createdAt": created})
	}
	c.JSON(200, gin.H{"items": out})
}
func (h storageHandler) Download(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.Status(401)
		return
	}
	var key, typ string
	if e = h.db.QueryRow(c, `SELECT storage_key,content_type FROM user_files WHERE id=$1 AND user_id=$2`, c.Param("id"), uid).Scan(&key, &typ); e != nil {
		c.Status(404)
		return
	}
	r, err := h.store.Open(c, key)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer r.Close()
	c.DataFromReader(200, -1, typ, r, nil)
}
func (h storageHandler) Delete(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.Status(401)
		return
	}
	var key string
	if e = h.db.QueryRow(c, `SELECT storage_key FROM user_files WHERE id=$1 AND user_id=$2`, c.Param("id"), uid).Scan(&key); e != nil {
		c.Status(404)
		return
	}
	if e = h.store.Delete(c, key); e != nil {
		c.Status(500)
		return
	}
	_, e = h.db.Exec(c, `DELETE FROM user_files WHERE id=$1 AND user_id=$2`, c.Param("id"), uid)
	if e != nil {
		c.Status(500)
		return
	}
	c.Status(204)
}
