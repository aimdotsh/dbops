package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/software"
	"github.com/gin-gonic/gin"
)

func (s *Server) listSoftwarePackages(c *gin.Context) {
	items, err := s.software.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) uploadSoftwarePackage(c *gin.Context) {
	if c.Request.ContentLength > 8<<30 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "PAYLOAD_TOO_LARGE", "message": "package exceeds 8 GiB"})
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "file is required"})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		fail(c, err)
		return
	}
	defer src.Close()

	req := software.StoreRequest{
		SoftwareName:      c.PostForm("software_name"),
		Version:           c.PostForm("version"),
		OSFamily:          c.PostForm("os_family"),
		Architecture:      c.PostForm("architecture"),
		PackageType:       c.PostForm("package_type"),
		FileName:          fileHeader.Filename,
		CompatibilityJSON: strings.TrimSpace(c.PostForm("compatibility_json")),
	}
	pkg, err := s.software.Store(c.Request.Context(), req, src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "PACKAGE_UPLOAD_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": pkg})
}

func (s *Server) downloadSoftwarePackage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": "invalid package id"})
		return
	}
	if err := s.software.ServeDownload(c.Writer, c.Request, id); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": "PACKAGE_DOWNLOAD_DENIED", "message": err.Error()})
	}
}

func (s *Server) createMySQLInstall(c *gin.Context) {
	var req mysqlinstall.InstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	task, err := s.mysqlInstaller.CreateTask(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MYSQL_INSTALL_REJECTED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": "OK", "message": "accepted", "data": task})
}
