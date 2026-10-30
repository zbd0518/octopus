package relay

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/relay/condition"
)

func routingConditionContext(c *gin.Context, modelName string) condition.RequestContext {
	headers := make(map[string]string, len(c.Request.Header))
	for name := range c.Request.Header {
		headers[strings.ToLower(name)] = c.Request.Header.Get(name)
	}
	return condition.RequestContext{
		Model: modelName, APIKeyID: c.GetInt("api_key_id"),
		APIKeyName: c.GetString("api_key_name"), Headers: headers,
		ContentType: c.ContentType(), Hour: time.Now().UTC().Hour(),
	}
}
