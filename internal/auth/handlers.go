package auth

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

type credentials struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"displayName"`
}

func (h *Handler) Register(c *gin.Context) {
	var in credentials
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	u, a, r, e := h.service.Register(c, in.Email, in.Password, in.DisplayName)
	if e != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered or invalid credentials"})
		return
	}
	_ = h.service.RecordSession(c, u.ID, r)
	_, _ = h.service.issueToken(c, u.ID, "verify_email")
	c.JSON(http.StatusCreated, gin.H{"user": u, "accessToken": a, "refreshToken": r})
}
func (h *Handler) VerifyEmail(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || h.service.VerifyEmail(c, in.Token) != nil {
		c.JSON(400, gin.H{"error": "invalid or expired verification token"})
		return
	}
	c.Status(204)
}
func (h *Handler) ForgotPassword(c *gin.Context) {
	var in struct {
		Email string `json:"email"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Email == "" {
		c.JSON(400, gin.H{"error": "email is required"})
		return
	}
	if h.service.RequestPasswordReset(c, in.Email) != nil {
		c.JSON(500, gin.H{"error": "request failed"})
		return
	}
	c.Status(202)
}
func (h *Handler) ResetPassword(c *gin.Context) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || h.service.ResetPassword(c, in.Token, in.Password) != nil {
		c.JSON(400, gin.H{"error": "invalid or expired reset token"})
		return
	}
	c.Status(204)
}
func (h *Handler) Login(c *gin.Context) {
	var in credentials
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	u, a, r, e := h.service.Login(c, in.Email, in.Password)
	if e != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	_ = h.service.RecordSession(c, u.ID, r)
	c.JSON(http.StatusOK, gin.H{"user": u, "accessToken": a, "refreshToken": r})
}
func (h *Handler) Apple(c *gin.Context) {
	var in struct {
		IdentityToken string `json:"identityToken"`
		DisplayName   string `json:"displayName"`
		Nonce         string `json:"nonce"`
	}
	if c.ShouldBindJSON(&in) != nil || in.IdentityToken == "" {
		c.JSON(400, gin.H{"error": "identityToken is required"})
		return
	}
	u, a, r, e := h.service.AppleLogin(c, in.IdentityToken, in.DisplayName, in.Nonce)
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid Apple identity token"})
		return
	}
	_ = h.service.RecordSession(c, u.ID, r)
	c.JSON(200, gin.H{"user": u, "accessToken": a, "refreshToken": r})
}
func (h *Handler) LinkApple(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	var in struct {
		IdentityToken string `json:"identityToken"`
		Nonce         string `json:"nonce"`
	}
	if err != nil || c.ShouldBindJSON(&in) != nil || in.IdentityToken == "" || h.service.LinkApple(c, uid, in.IdentityToken, in.Nonce) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Apple account cannot be linked"})
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *Handler) UnlinkApple(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil || h.service.UnlinkApple(c, uid) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Apple account cannot be unlinked"})
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *Handler) Refresh(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refreshToken" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refreshToken is required"})
		return
	}
	token, err := h.service.Refresh(in.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"accessToken": token})
}
func (h *Handler) Revoke(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	if c.ShouldBindJSON(&in) != nil || in.RefreshToken == "" {
		c.JSON(400, gin.H{"error": "refreshToken is required"})
		return
	}
	if h.service.RevokeRefresh(c, in.RefreshToken) != nil {
		c.JSON(500, gin.H{"error": "revoke failed"})
		return
	}
	c.Status(204)
}
func (h *Handler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": gin.H{"id": c.GetString("userID"), "role": c.GetString("role")}})
}
func (h *Handler) Sessions(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	rows, err := h.service.db.Query(c, `SELECT id,expires_at,created_at,revoked_at FROM auth_sessions WHERE user_id=$1 ORDER BY created_at DESC`, uid)
	if err != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id string
		var expires, created, revoked any
		if rows.Scan(&id, &expires, &created, &revoked) == nil {
			out = append(out, gin.H{"id": id, "expiresAt": expires, "createdAt": created, "revokedAt": revoked, "current": false})
		}
	}
	c.JSON(200, gin.H{"sessions": out})
}
func (h *Handler) RevokeSession(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	sid, sidErr := uuid.Parse(c.Param("id"))
	if err != nil || sidErr != nil {
		c.JSON(400, gin.H{"error": "invalid session"})
		return
	}
	tag, err := h.service.db.Exec(c, `UPDATE auth_sessions SET revoked_at=NOW() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, sid, uid)
	if err != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}
	c.Status(204)
}
func (h *Handler) ChangePassword(c *gin.Context) {
	id, e := uuid.Parse(c.GetString("userID"))
	var in struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if e != nil || c.ShouldBindJSON(&in) != nil || h.service.ChangePassword(c, id, in.OldPassword, in.NewPassword) != nil {
		c.JSON(400, gin.H{"error": "password change failed"})
		return
	}
	c.Status(204)
}
func (h *Handler) Export(c *gin.Context) {
	id, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	u, e := h.service.Export(c, id)
	if e != nil {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	data := gin.H{"user": u}
	var progress json.RawMessage
	if h.service.db.QueryRow(c, `SELECT payload FROM learning_progress WHERE user_id=$1`, id).Scan(&progress) == nil {
		data["progress"] = progress
	}
	devices := []gin.H{}
	rows, _ := h.service.db.Query(c, `SELECT id,platform,name,last_seen_at FROM devices WHERE user_id=$1`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var did, platform, name string
			var seen any
			if rows.Scan(&did, &platform, &name, &seen) == nil {
				devices = append(devices, gin.H{"id": did, "platform": platform, "name": name, "lastSeenAt": seen})
			}
		}
	}
	data["devices"] = devices
	files := []gin.H{}
	if rows, err := h.service.db.Query(c, `SELECT id,name,size_bytes,content_type,safety_status,created_at FROM user_files WHERE user_id=$1 ORDER BY created_at`, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var fid, name, typ, status string
			var size int64
			var created any
			if rows.Scan(&fid, &name, &size, &typ, &status, &created) == nil {
				files = append(files, gin.H{"id": fid, "name": name, "size": size, "contentType": typ, "safetyStatus": status, "createdAt": created})
			}
		}
	}
	data["files"] = files
	usage := []gin.H{}
	if rows, err := h.service.db.Query(c, `SELECT model,provider_status,input_tokens,output_tokens,cost_micros,created_at FROM ai_usage WHERE user_id=$1 ORDER BY created_at`, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var model string
			var status, inTok, outTok int
			var cost int64
			var created any
			if rows.Scan(&model, &status, &inTok, &outTok, &cost, &created) == nil {
				usage = append(usage, gin.H{"model": model, "providerStatus": status, "inputTokens": inTok, "outputTokens": outTok, "costMicros": cost, "createdAt": created})
			}
		}
	}
	data["aiUsage"] = usage
	events := []gin.H{}
	if rows, err := h.service.db.Query(c, `SELECT id,event_type,payload,client_created_at,created_at,deleted_at,field_timestamps FROM sync_events WHERE user_id=$1 ORDER BY created_at`, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var eid, typ string
			var payload, timestamps []byte
			var client, created time.Time
			var deleted *time.Time
			if rows.Scan(&eid, &typ, &payload, &client, &created, &deleted, &timestamps) == nil {
				events = append(events, gin.H{"id": eid, "eventType": typ, "payload": json.RawMessage(payload), "clientCreatedAt": client, "createdAt": created, "deletedAt": deleted, "fieldTimestamps": json.RawMessage(timestamps)})
			}
		}
	}
	data["syncEvents"] = events
	c.JSON(200, data)
}
func (h *Handler) HardDelete(c *gin.Context) {
	id, e := uuid.Parse(c.GetString("userID"))
	var in struct {
		Confirm string `json:"confirm"`
	}
	if e != nil || c.ShouldBindJSON(&in) != nil || in.Confirm != "DELETE MY ACCOUNT" {
		c.JSON(400, gin.H{"error": "confirm must equal DELETE MY ACCOUNT"})
		return
	}
	if h.service.HardDelete(c, id) != nil {
		c.JSON(500, gin.H{"error": "hard delete failed"})
		return
	}
	c.Status(204)
}
func (h *Handler) DeleteAccount(c *gin.Context) {
	id, err := uuid.Parse(c.GetString("userID"))
	if err != nil || h.service.Delete(c, id) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete account failed"})
		return
	}
	_, _ = h.service.db.Exec(c, `UPDATE auth_sessions SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	c.Status(http.StatusNoContent)
}
func (h *Handler) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		id, role, e := h.service.Parse(strings.TrimPrefix(raw, "Bearer "))
		if e != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set("userID", id.String())
		c.Set("role", role)
		c.Next()
	}
}
