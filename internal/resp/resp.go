// Package resp 统一响应包与业务错误码。
// 响应格式：{"code":0,"message":"ok","data":{...},"request_id":"..."}
package resp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 业务码：0 成功；4xxxx 客户端侧；5xxxx 服务端/上游侧。
const (
	CodeOK              = 0
	CodeBadRequest      = 40001 // 参数错误
	CodeInvalidKey      = 40101 // 无效或缺失 API Key
	CodeForbidden       = 40301 // 需要 admin 权限
	CodeNotFound        = 40004 // 未知的 spider / vendor
	CodeRateLimited     = 42901 // 触发限流
	CodeBusy            = 42902 // 爬虫并发槽位满
	CodeInternal        = 50000 // 服务内部错误
	CodeUpstreamTimeout = 50011 // 抓取/上游任务超时
	CodeUpstreamParse   = 50012 // 目标响应解析失败
	CodeUpstreamFetch   = 50013 // 目标站/上游请求失败
	CodeUpstreamMissing = 50014 // 上游资源不存在
)

// CtxBizCode 写入 gin context 的业务码键，供访问日志读取。
const CtxBizCode = "biz_code"

type Body struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func OK(c *gin.Context, data any) {
	c.Set(CtxBizCode, CodeOK)
	c.JSON(http.StatusOK, Body{Code: CodeOK, Message: "ok", Data: data, RequestID: requestID(c)})
}

// Fail 按业务码映射 HTTP 状态码并结束请求。
func Fail(c *gin.Context, code int, message string) {
	c.Set(CtxBizCode, code)
	c.AbortWithStatusJSON(httpStatus(code), Body{Code: code, Message: message, RequestID: requestID(c)})
}

func httpStatus(code int) int {
	switch code {
	case CodeBadRequest:
		return http.StatusBadRequest
	case CodeInvalidKey:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound, CodeUpstreamMissing:
		return http.StatusNotFound
	case CodeRateLimited, CodeBusy:
		return http.StatusTooManyRequests
	case CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case CodeUpstreamParse, CodeUpstreamFetch:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
