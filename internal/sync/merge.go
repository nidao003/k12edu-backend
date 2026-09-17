package sync

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
)

type mergeInput struct {
	Base   json.RawMessage `json:"base"`
	Local  json.RawMessage `json:"local"`
	Remote json.RawMessage `json:"remote"`
}

func mergeValue(local, remote any) any {
	lm, lok := local.(map[string]any)
	rm, rok := remote.(map[string]any)
	if lok && rok {
		out := map[string]any{}
		for k, v := range rm {
			out[k] = v
		}
		for k, v := range lm {
			if old, ok := out[k]; ok {
				out[k] = mergeValue(v, old)
			} else {
				out[k] = v
			}
		}
		return out
	}
	if la, lok := local.([]any); lok {
		if ra, rok := remote.([]any); rok {
			out := append([]any{}, ra...)
			seen := map[string]bool{}
			for _, v := range out {
				b, _ := json.Marshal(v)
				seen[string(b)] = true
			}
			for _, v := range la {
				b, _ := json.Marshal(v)
				if !seen[string(b)] {
					out = append(out, v)
				}
			}
			return out
		}
	}
	return local
}
func (h *Handler) Merge(c *gin.Context) {
	var in mergeInput
	if c.ShouldBindJSON(&in) != nil || !json.Valid(in.Local) || !json.Valid(in.Remote) {
		c.JSON(400, gin.H{"error": "local and remote must be valid JSON"})
		return
	}
	var l, r any
	_ = json.Unmarshal(in.Local, &l)
	_ = json.Unmarshal(in.Remote, &r)
	out, _ := json.Marshal(mergeValue(l, r))
	c.JSON(200, gin.H{"payload": json.RawMessage(out), "strategy": "object-recursive-local-wins-arrays-deduplicated"})
}
