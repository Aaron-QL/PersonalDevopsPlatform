package httputil

import "github.com/gin-gonic/gin"

type successResponse struct {
	Code int `json:"code"`
	Data any `json:"data"`
}

type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func OK(c *gin.Context, data any) {
	c.JSON(200, successResponse{Code: 0, Data: data})
}

func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, errorResponse{Code: 1, Message: message})
}
