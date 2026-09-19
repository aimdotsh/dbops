package software

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
)

type StoreRequest struct {
	SoftwareName      string
	Version           string
	OSFamily          string
	Architecture      string
	PackageType       string
	FileName          string
	CompatibilityJSON string
}

type Service struct {
	repo       repository.SoftwarePackageRepository
	baseDir    string
	publicURL  string
	signingKey []byte
}

func New(repo repository.SoftwarePackageRepository, baseDir, publicURL, signingKey string) *Service {
	return &Service{
		repo: repo, baseDir: baseDir, publicURL: strings.TrimRight(publicURL, "/"),
		signingKey: []byte(signingKey),
	}
}

func (s *Service) List(ctx context.Context) ([]domain.SoftwarePackage, error) {
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (domain.SoftwarePackage, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Store(ctx context.Context, req StoreRequest, src io.Reader) (domain.SoftwarePackage, error) {
	req.SoftwareName = strings.TrimSpace(req.SoftwareName)
	req.Version = strings.TrimSpace(req.Version)
	req.OSFamily = strings.ToLower(strings.TrimSpace(req.OSFamily))
	req.Architecture = strings.ToLower(strings.TrimSpace(req.Architecture))
	req.PackageType = strings.ToLower(strings.TrimSpace(req.PackageType))
	req.FileName = filepath.Base(strings.TrimSpace(req.FileName))

	if req.SoftwareName == "" || req.Version == "" || req.OSFamily == "" || req.Architecture == "" || req.FileName == "" {
		return domain.SoftwarePackage{}, errors.New("software_name, version, os_family, architecture and file_name are required")
	}
	if req.PackageType == "" {
		req.PackageType = inferPackageType(req.FileName)
	}
	if req.PackageType != "tar.gz" && req.PackageType != "tgz" {
		return domain.SoftwarePackage{}, fmt.Errorf("unsupported package_type %q; V1 MySQL installer supports tar.gz/tgz", req.PackageType)
	}
	if req.CompatibilityJSON == "" {
		req.CompatibilityJSON = "{}"
	}

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return domain.SoftwarePackage{}, err
	}
	tmp, err := os.CreateTemp(s.baseDir, ".upload-*")
	if err != nil {
		return domain.SoftwarePackage{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, 8<<30))
	closeErr := tmp.Close()
	if copyErr != nil {
		return domain.SoftwarePackage{}, copyErr
	}
	if closeErr != nil {
		return domain.SoftwarePackage{}, closeErr
	}
	if n == 0 {
		return domain.SoftwarePackage{}, errors.New("package is empty")
	}
	sum := hex.EncodeToString(h.Sum(nil))

	dir := filepath.Join(
		s.baseDir,
		safeSegment(req.SoftwareName),
		safeSegment(req.Version),
		safeSegment(req.OSFamily+"-"+req.Architecture),
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return domain.SoftwarePackage{}, err
	}
	finalPath := filepath.Join(dir, sum[:16]+"-"+req.FileName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return domain.SoftwarePackage{}, err
	}
	_ = os.Chmod(finalPath, 0o644)

	p, err := s.repo.Create(ctx, domain.SoftwarePackage{
		SoftwareName:      req.SoftwareName,
		Version:           req.Version,
		OSFamily:          req.OSFamily,
		Architecture:      req.Architecture,
		PackageType:       req.PackageType,
		FileName:          req.FileName,
		StoragePath:       finalPath,
		SHA256:            sum,
		SizeBytes:         n,
		CompatibilityJSON: req.CompatibilityJSON,
		Status:            "available",
	})
	if err != nil {
		_ = os.Remove(finalPath)
		return domain.SoftwarePackage{}, err
	}
	return p, nil
}

func (s *Service) SignedDownloadURL(id int64, ttl time.Duration) string {
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 30 * time.Minute
	}
	expires := time.Now().UTC().Add(ttl).Unix()
	sig := s.sign(id, expires)
	return fmt.Sprintf("%s/api/v1/software/packages/%d/download?expires=%d&sig=%s", s.publicURL, id, expires, sig)
}

func (s *Service) ServeDownload(w http.ResponseWriter, r *http.Request, id int64) error {
	expires, err := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if err != nil || expires < time.Now().UTC().Unix() {
		return errors.New("download token expired or invalid")
	}
	if expires > time.Now().UTC().Add(24*time.Hour).Unix() {
		return errors.New("download token expiry exceeds maximum")
	}
	expected := s.sign(id, expires)
	provided := r.URL.Query().Get("sig")
	if !hmac.Equal([]byte(expected), []byte(provided)) {
		return errors.New("invalid download signature")
	}
	p, err := s.repo.Get(r.Context(), id)
	if err != nil {
		return err
	}
	if p.Status != "available" {
		return errors.New("package is not available")
	}
	info, err := os.Stat(p.StoragePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("package storage path is not a regular file")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", p.FileName))
	w.Header().Set("X-DBOps-SHA256", p.SHA256)
	http.ServeFile(w, r, p.StoragePath)
	return nil
}

func (s *Service) sign(id int64, expires int64) string {
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = fmt.Fprintf(mac, "%d:%d", id, expires)
	return hex.EncodeToString(mac.Sum(nil))
}

func inferPackageType(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tar.gz") {
		return "tar.gz"
	}
	if strings.HasSuffix(lower, ".tgz") {
		return "tgz"
	}
	return ""
}

func safeSegment(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
