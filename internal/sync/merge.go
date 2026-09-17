package sync

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"strings"
)

type mergeInput struct {
	Base   json.RawMessage `json:"base"`
	Local  json.RawMessage `json:"local"`
	Remote json.RawMessage `json:"remote"`
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func itemKey(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range []string{"id", "lessonId", "questionId", "subjectId", "recordId"} {
		if x, ok := m[k]; ok {
			return k + ":" + toString(x)
		}
	}
	return ""
}
func toString(v any) string { b, _ := json.Marshal(v); return string(b) }
func isCounter(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, "count") || strings.HasSuffix(n, "seconds") || strings.HasSuffix(n, "duration") || strings.HasSuffix(n, "score")
}
func mergeField(name string, base, local, remote any) any {
	if equalJSON(local, remote) {
		return local
	}
	if equalJSON(local, base) {
		return remote
	}
	if equalJSON(remote, base) {
		return local
	}
	lm, lok := local.(map[string]any)
	rm, rok := remote.(map[string]any)
	bm, _ := base.(map[string]any)
	if lok && rok {
		out := map[string]any{}
		for k, v := range rm {
			out[k] = v
		}
		for k, v := range lm {
			bv := any(nil)
			if bm != nil {
				bv = bm[k]
			}
			rv := rm[k]
			out[k] = mergeField(k, bv, v, rv)
		}
		return out
	}
	la, lok := local.([]any)
	ra, rok := remote.([]any)
	if lok && rok {
		out := append([]any{}, ra...)
		idx := map[string]int{}
		for i, v := range out {
			if k := itemKey(v); k != "" {
				idx[k] = i
			}
		}
		for _, v := range la {
			if k := itemKey(v); k != "" {
				if i, ok := idx[k]; ok {
					var old any
					if bm, ok := base.([]any); ok {
						for _, x := range bm {
							if itemKey(x) == k {
								old = x
								break
							}
						}
					}
					out[i] = mergeField(name, old, v, out[i])
				} else {
					idx[k] = len(out)
					out = append(out, v)
				}
			} else {
				found := false
				for _, x := range out {
					if equalJSON(x, v) {
						found = true
						break
					}
				}
				if !found {
					out = append(out, v)
				}
			}
		}
		return out
	}
	if isCounter(name) {
		lf, lok := local.(float64)
		rf, rok := remote.(float64)
		bf, bok := base.(float64)
		if lok && rok && bok {
			return bf + (lf - bf) + (rf - bf)
		}
	}
	return local
}
func (h *Handler) Merge(c *gin.Context) {
	var in mergeInput
	if c.ShouldBindJSON(&in) != nil || !json.Valid(in.Base) || !json.Valid(in.Local) || !json.Valid(in.Remote) {
		c.JSON(400, gin.H{"error": "base, local and remote must be valid JSON"})
		return
	}
	var b, l, r any
	_ = json.Unmarshal(in.Base, &b)
	_ = json.Unmarshal(in.Local, &l)
	_ = json.Unmarshal(in.Remote, &r)
	out, _ := json.Marshal(mergeField("", b, l, r))
	c.JSON(200, gin.H{"payload": json.RawMessage(out), "strategy": "three-way-field-merge", "tombstone": "deletedAt"})
}
