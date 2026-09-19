package httpapi

import (
	"net/http"
	"strconv"
	"time"

	metricstore "github.com/aimdotsh/dbops/internal/metrics"
	"github.com/gin-gonic/gin"
)

func (s *Server) getLatestMetric(c *gin.Context) {
	resourceType := c.Query("resource_type")
	resourceID, err := strconv.ParseInt(c.Query("resource_id"), 10, 64)
	if resourceType == "" || err != nil || resourceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "resource_type and positive resource_id are required"})
		return
	}
	snap, err := s.metrics.Latest(c.Request.Context(), resourceType, resourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "METRIC_NOT_FOUND", "message": err.Error()})
		return
	}
	ok(c, snap)
}

func (s *Server) getMetricRange(c *gin.Context) {
	resourceType := c.Query("resource_type")
	resourceID, err := strconv.ParseInt(c.Query("resource_id"), 10, 64)
	if resourceType == "" || err != nil || resourceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "resource_type and positive resource_id are required"})
		return
	}
	granularity := c.DefaultQuery("granularity", "5m")
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if raw := c.Query("from"); raw != "" {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "from must be RFC3339"})
			return
		}
		from = v
	}
	if raw := c.Query("to"); raw != "" {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "to must be RFC3339"})
			return
		}
		to = v
	}
	limit := 1000
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	items, err := s.metrics.Range(c.Request.Context(), granularity, resourceType, resourceID, from, to, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "METRIC_QUERY_FAILED", "message": err.Error()})
		return
	}
	ok(c, items)
}

var _ = metricstore.Snapshot{}
