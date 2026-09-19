package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type SoftwarePackageRepo struct{ DB *sql.DB }

func (r SoftwarePackageRepo) Create(ctx context.Context, p domain.SoftwarePackage) (domain.SoftwarePackage, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.Status == "" {
		p.Status = "available"
	}
	if p.CompatibilityJSON == "" {
		p.CompatibilityJSON = "{}"
	}
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO software_packages(
  software_name,version,os_family,architecture,package_type,file_name,storage_path,download_url,
  sha256,size_bytes,compatibility_json,status,created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.SoftwareName, p.Version, p.OSFamily, p.Architecture, p.PackageType, p.FileName,
		p.StoragePath, nullString(p.DownloadURL), p.SHA256, p.SizeBytes, p.CompatibilityJSON, p.Status, now)
	if err != nil {
		return p, err
	}
	p.ID, _ = res.LastInsertId()
	p.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return p, nil
}

func (r SoftwarePackageRepo) Get(ctx context.Context, id int64) (domain.SoftwarePackage, error) {
	var p domain.SoftwarePackage
	var created string
	err := r.DB.QueryRowContext(ctx, `
SELECT id,software_name,version,os_family,architecture,package_type,file_name,storage_path,
       COALESCE(download_url,''),sha256,size_bytes,compatibility_json,status,created_at
FROM software_packages WHERE id=?`, id).
		Scan(&p.ID, &p.SoftwareName, &p.Version, &p.OSFamily, &p.Architecture, &p.PackageType,
			&p.FileName, &p.StoragePath, &p.DownloadURL, &p.SHA256, &p.SizeBytes,
			&p.CompatibilityJSON, &p.Status, &created)
	if err == nil {
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	return p, err
}

func (r SoftwarePackageRepo) List(ctx context.Context) ([]domain.SoftwarePackage, error) {
	rows, err := r.DB.QueryContext(ctx, `
SELECT id,software_name,version,os_family,architecture,package_type,file_name,storage_path,
       COALESCE(download_url,''),sha256,size_bytes,compatibility_json,status,created_at
FROM software_packages ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SoftwarePackage
	for rows.Next() {
		var p domain.SoftwarePackage
		var created string
		if err := rows.Scan(&p.ID, &p.SoftwareName, &p.Version, &p.OSFamily, &p.Architecture, &p.PackageType,
			&p.FileName, &p.StoragePath, &p.DownloadURL, &p.SHA256, &p.SizeBytes,
			&p.CompatibilityJSON, &p.Status, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, p)
	}
	return out, rows.Err()
}
