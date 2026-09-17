package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strings"
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
	c.JSON(http.StatusCreated, gin.H{"user": u, "accessToken": a, "refreshToken": r})
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
	c.JSON(http.StatusOK, gin.H{"user": u, "accessToken": a, "refreshToken": r})
}
func (h *Handler) Apple(c *gin.Context) {
	var in struct {
		IdentityToken string `json:"identityToken"`
		DisplayName   string `json:"displayName"`
	}
	if c.ShouldBindJSON(&in) != nil || in.IdentityToken == "" {
		c.JSON(400, gin.H{"error": "identityToken is required"})
		return
	}
	u, a, r, e := h.service.AppleLogin(c, in.IdentityToken, in.DisplayName)
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid Apple identity token"})
		return
	}
	c.JSON(200, gin.H{"user": u, "accessToken": a, "refreshToken": r})
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
func (h *Handler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": gin.H{"id": c.GetString("userID"), "role": c.GetString("role")}})
}
func (h *Handler) DeleteAccount(c *gin.Context) {
	id, err := uuid.Parse(c.GetString("userID"))
	if err != nil || h.service.Delete(c, id) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete account failed"})
		return
	}
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
