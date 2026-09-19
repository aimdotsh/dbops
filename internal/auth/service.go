package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleSuperAdmin = "SuperAdmin"
	RoleDBA        = "DBA"
	RoleOperator   = "Operator"
	RoleViewer     = "Viewer"
	RoleAuditor    = "Auditor"
)

type Config struct {
	Enabled                bool
	JWTSecret              string
	AccessTTL              time.Duration
	RefreshTTL             time.Duration
	BootstrapAdminUsername string
	BootstrapAdminPassword string
}

type Service struct {
	repo repository.AuthRepository
	cfg  Config
}

type Claims struct {
	UserID   int64    `json:"uid"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	Type     string   `json:"typ"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    int64       `json:"expires_in"`
	User         domain.User `json:"user"`
}

type CreateUserRequest struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	Role        string
}

func New(repo repository.AuthRepository, cfg Config) (*Service, error) {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	if cfg.BootstrapAdminUsername == "" {
		cfg.BootstrapAdminUsername = "admin"
	}
	if cfg.Enabled && len(cfg.JWTSecret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 characters")
	}
	return &Service{repo: repo, cfg: cfg}, nil
}

func (s *Service) Enabled() bool { return s.cfg.Enabled }

func (s *Service) Bootstrap(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := validatePassword(s.cfg.BootstrapAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin password is required for first startup: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(s.cfg.BootstrapAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateUser(ctx, domain.User{
		Username:     normalizeUsername(s.cfg.BootstrapAdminUsername),
		PasswordHash: string(hash),
		DisplayName:  "DBOps Administrator",
		Status:       "active",
	}, RoleSuperAdmin)
	return err
}

func (s *Service) Login(ctx context.Context, username, password string) (TokenPair, error) {
	if !s.cfg.Enabled {
		return TokenPair{}, errors.New("authentication is disabled")
	}
	user, err := s.repo.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return TokenPair{}, errors.New("invalid username or password")
	}
	if user.Status != "active" {
		return TokenPair{}, errors.New("user is disabled")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return TokenPair{}, errors.New("invalid username or password")
	}
	if err := s.repo.UpdateLastLogin(ctx, user.ID); err != nil {
		return TokenPair{}, err
	}
	user.PasswordHash = ""
	return s.issuePair(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	if !s.cfg.Enabled {
		return TokenPair{}, errors.New("authentication is disabled")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return TokenPair{}, errors.New("refresh token is required")
	}
	now := time.Now().UTC()
	userID, err := s.repo.ConsumeRefreshToken(ctx, tokenHash(refreshToken), now.Format(time.RFC3339))
	if err != nil {
		return TokenPair{}, errors.New("invalid or expired refresh token")
	}
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		return TokenPair{}, err
	}
	if user.Status != "active" {
		return TokenPair{}, errors.New("user is disabled")
	}
	user.PasswordHash = ""
	return s.issuePair(ctx, user)
}

func (s *Service) ParseAccessToken(raw string) (Claims, error) {
	var claims Claims
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", token.Method.Alg())
		}
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithIssuer("dbops"), jwt.WithAudience("dbops-api"))
	if err != nil || !token.Valid || claims.Type != "access" || claims.UserID <= 0 {
		return Claims{}, errors.New("invalid access token")
	}
	return claims, nil
}

func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (domain.User, error) {
	username := normalizeUsername(req.Username)
	if username == "" || len(username) > 64 {
		return domain.User{}, errors.New("invalid username")
	}
	if err := validatePassword(req.Password); err != nil {
		return domain.User{}, err
	}
	if !validRole(req.Role) {
		return domain.User{}, errors.New("invalid role")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	return s.repo.CreateUser(ctx, domain.User{
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Email:        strings.TrimSpace(req.Email),
		Status:       "active",
	}, req.Role)
}

func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	items, err := s.repo.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].PasswordHash = ""
	}
	return items, nil
}

func (s *Service) GetUser(ctx context.Context, id int64) (domain.User, error) {
	u, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return u, err
	}
	u.PasswordHash = ""
	return u, nil
}

func (s *Service) issuePair(ctx context.Context, user domain.User) (TokenPair, error) {
	now := time.Now().UTC()
	exp := now.Add(s.cfg.AccessTTL)
	claims := Claims{
		UserID: user.ID, Username: user.Username, Roles: user.Roles, Type: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dbops",
			Subject:   fmt.Sprintf("%d", user.ID),
			Audience:  jwt.ClaimStrings{"dbops-api"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := randomToken(32)
	if err != nil {
		return TokenPair{}, err
	}
	refreshExp := now.Add(s.cfg.RefreshTTL)
	if err := s.repo.StoreRefreshToken(ctx, user.ID, tokenHash(refresh), refreshExp.Format(time.RFC3339)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresIn: int64(s.cfg.AccessTTL.Seconds()), User: user,
	}, nil
}

func HasAnyRole(roles []string, allowed ...string) bool {
	set := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		set[role] = struct{}{}
	}
	for _, role := range allowed {
		if _, ok := set[role]; ok {
			return true
		}
	}
	return false
}

func validRole(role string) bool {
	switch role {
	case RoleSuperAdmin, RoleDBA, RoleOperator, RoleViewer, RoleAuditor:
		return true
	default:
		return false
	}
}

func validatePassword(v string) error {
	if len(v) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(v) > 128 {
		return errors.New("password is too long")
	}
	return nil
}

func normalizeUsername(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '@' {
			continue
		}
		return ""
	}
	return v
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
