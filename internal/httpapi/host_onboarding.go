package httpapi

import (
	"errors"
	"net/http"

	"github.com/aimdotsh/dbops/internal/hostonboarding"
	"github.com/gin-gonic/gin"
)

func (s *Server) precheckHostOnboarding(c *gin.Context) {
	var req hostonboarding.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "请求参数格式无效"})
		return
	}
	result, err := s.hostOnboarding.Precheck(c.Request.Context(), req)
	if err != nil {
		hostOnboardingFail(c, err)
		return
	}
	ok(c, result)
}

func (s *Server) onboardHost(c *gin.Context) {
	var req hostonboarding.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "请求参数格式无效"})
		return
	}
	result, err := s.hostOnboarding.Onboard(c.Request.Context(), req)
	if err != nil {
		hostOnboardingFail(c, err)
		return
	}
	ok(c, result)
}

func hostOnboardingFail(c *gin.Context, err error) {
	var validation hostonboarding.ValidationError
	if errors.As(err, &validation) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": validation.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "HOST_ONBOARDING_FAILED", "message": err.Error()})
}
