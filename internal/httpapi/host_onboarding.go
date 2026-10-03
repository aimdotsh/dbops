package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/hostonboarding"
	"github.com/gin-gonic/gin"
)

func (s *Server) diagnoseHostSSH(c *gin.Context) {
	id, valid := parseID(c)
	if !valid {
		return
	}
	host, err := s.hosts.Get(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "纳管主机不存在"})
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	var req hostonboarding.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "请求参数格式无效"})
		return
	}
	all, err := s.dbs.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	instances := make([]domain.DatabaseInstance, 0)
	for _, instance := range all {
		if instance.HostID == id {
			instances = append(instances, instance)
		}
	}
	result, err := s.hostOnboarding.Diagnose(c.Request.Context(), req, host, instances)
	if err != nil {
		hostOnboardingFail(c, err)
		return
	}
	ok(c, result)
}

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

func (s *Server) checkHostOnboardingConnectivity(c *gin.Context) {
	var req hostonboarding.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "请求参数格式无效"})
		return
	}
	result, err := s.hostOnboarding.CheckConnectivity(c.Request.Context(), req)
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
