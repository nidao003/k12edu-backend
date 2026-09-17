package ai

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strings"
)

type Handler struct {
	baseURL, apiKey string
	client          *http.Client
}

func NewHandler(baseURL, apiKey string) *Handler {
	return &Handler{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{}}
}
func (h *Handler) Chat(c *gin.Context) {
	if h.baseURL == "" || h.apiKey == "" {
		c.JSON(503, gin.H{"error": "AI provider is not configured"})
		return
	}
	body, e := io.ReadAll(c.Request.Body)
	if e != nil || !json.Valid(body) {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	req, e := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, h.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if e != nil {
		c.JSON(500, gin.H{"error": "create request failed"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, e := h.client.Do(req)
	if e != nil {
		c.JSON(502, gin.H{"error": "AI provider unavailable"})
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	c.Data(resp.StatusCode, "application/json", data)
}
