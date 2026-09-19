package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type DatabaseRepo struct{ DB *sql.DB }

func (r DatabaseRepo) List(ctx context.Context) ([]domain.DatabaseInstance, error) {
	rows, err := r.DB.QueryContext(ctx, `
SELECT id,name,db_type,COALESCE(version,''),COALESCE(host_id,0),COALESCE(port,0),COALESCE(role,''),
       COALESCE(data_dir,''),COALESCE(config_path,''),credential_id,status,managed_mode,COALESCE(metadata_json,'{}')
FROM database_instances ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DatabaseInstance
	for rows.Next() {
		d, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r DatabaseRepo) Get(ctx context.Context, id int64) (domain.DatabaseInstance, error) {
	return scanDatabase(r.DB.QueryRowContext(ctx, `
SELECT id,name,db_type,COALESCE(version,''),COALESCE(host_id,0),COALESCE(port,0),COALESCE(role,''),
       COALESCE(data_dir,''),COALESCE(config_path,''),credential_id,status,managed_mode,COALESCE(metadata_json,'{}')
FROM database_instances WHERE id=?`, id))
}

func (r DatabaseRepo) CreateInstalled(ctx context.Context, d domain.DatabaseInstance) (domain.DatabaseInstance, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if d.Status == "" {
		d.Status = "online"
	}
	if d.ManagedMode == "" {
		d.ManagedMode = "installed"
	}
	if d.MetadataJSON == "" {
		d.MetadataJSON = "{}"
	}
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO database_instances(name,db_type,version,host_id,port,role,data_dir,config_path,credential_id,status,managed_mode,metadata_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.Name, d.DBType, d.Version, d.HostID, d.Port, nullString(d.Role),
		nullString(d.DataDir), nullString(d.ConfigPath), d.CredentialID, d.Status, d.ManagedMode, d.MetadataJSON, now, now)
	if err != nil {
		return d, err
	}
	d.ID, _ = res.LastInsertId()
	return r.Get(ctx, d.ID)
}

type databaseScanner interface{ Scan(...any) error }

func scanDatabase(s databaseScanner) (domain.DatabaseInstance, error) {
	var d domain.DatabaseInstance
	var credentialID sql.NullInt64
	err := s.Scan(&d.ID, &d.Name, &d.DBType, &d.Version, &d.HostID, &d.Port, &d.Role,
		&d.DataDir, &d.ConfigPath, &credentialID, &d.Status, &d.ManagedMode, &d.MetadataJSON)
	if credentialID.Valid {
		v := credentialID.Int64
		d.CredentialID = &v
	}
	return d, err
}


func (r DatabaseRepo) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := r.DB.ExecContext(ctx, "UPDATE database_instances SET status=?,updated_at=? WHERE id=?",
		status, time.Now().UTC().Format(time.RFC3339), id)
	return err
}
