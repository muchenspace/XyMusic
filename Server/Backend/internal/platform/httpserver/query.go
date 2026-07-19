package httpserver

import "github.com/gin-gonic/gin"

// LastQueryValue matches the legacy Elysia scalar query contract: unknown
// fields are ignored by callers and repeated fields use the final value.
func LastQueryValue(c *gin.Context, name string) (string, bool) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "", false
	}
	values, present := c.Request.URL.Query()[name]
	if !present || len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}
