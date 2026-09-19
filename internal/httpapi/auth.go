package httpapi

import (
	"net/http"
	"strings"

	authsvc "github.com/aimdotsh/dbops/internal/auth"
	"github.com/gin-gonic/gin"
)

const authClaimsKey = "dbops.auth.claims"

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role"`
}

func (s *Server) login(c *gin.Context) {
	var body loginRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	pair, err := s.auth.Login(c.Request.Context(), body.Username, body.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "AUTH_FAILED", "message": "invalid username or password"})
		return
	}
	ok(c, pair)
}

func (s *Server) refreshToken(c *gin.Context) {
	var body refreshRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	pair, err := s.auth.Refresh(c.Request.Context(), body.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "REFRESH_FAILED", "message": "invalid or expired refresh token"})
		return
	}
	ok(c, pair)
}

func (s *Server) me(c *gin.Context) {
	claims, okClaims := currentClaims(c)
	if !okClaims {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED"})
		return
	}
	user, err := s.auth.GetUser(c.Request.Context(), claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED"})
		return
	}
	ok(c, user)
}

func (s *Server) createUser(c *gin.Context) {
	var body createUserRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMETER", "message": err.Error()})
		return
	}
	user, err := s.auth.CreateUser(c.Request.Context(), authsvc.CreateUserRequest{
		Username: body.Username, Password: body.Password, DisplayName: body.DisplayName,
		Email: body.Email, Role: body.Role,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "USER_CREATE_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "OK", "message": "created", "data": user})
}

func (s *Server) listUsers(c *gin.Context) {
	items, err := s.auth.ListUsers(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, items)
}

func (s *Server) authenticate(c *gin.Context) {
	if s.auth == nil || !s.auth.Enabled() {
		c.Next()
		return
	}
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Bearer token required"})
		return
	}
	claims, err := s.auth.ParseAccessToken(strings.TrimSpace(header[7:]))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid or expired access token"})
		return
	}
	c.Set(authClaimsKey, claims)
	c.Next()
}

func (s *Server) requireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.auth == nil || !s.auth.Enabled() {
			c.Next()
			return
		}
		claims, okClaims := currentClaims(c)
		if !okClaims {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED"})
			return
		}
		if !authsvc.HasAnyRole(claims.Roles, roles...) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "insufficient role"})
			return
		}
		c.Next()
	}
}

func currentClaims(c *gin.Context) (authsvc.Claims, bool) {
	v, ok := c.Get(authClaimsKey)
	if !ok {
		return authsvc.Claims{}, false
	}
	claims, ok := v.(authsvc.Claims)
	return claims, ok
}
