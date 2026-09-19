package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
)

type CredentialRepo struct{ DB *sql.DB }

func (r CredentialRepo) Create(ctx context.Context, c domain.Credential) (domain.Credential, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if c.MetadataJSON == "" {
		c.MetadataJSON = "{}"
	}
	res, err := r.DB.ExecContext(ctx, `
INSERT INTO credentials(name,credential_type,username,encrypted_secret,encryption_version,metadata_json,created_at,updated_at)
VALUES(?,?,?,?,1,?,?,?)`,
		c.Name, c.CredentialType, nullString(c.Username), c.EncryptedSecret, c.MetadataJSON, now, now)
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt, _ = time.Parse(time.RFC3339, now)
	c.UpdatedAt = c.CreatedAt
	return c, nil
}

func (r CredentialRepo) Get(ctx context.Context, id int64) (domain.Credential, error) {
	var c domain.Credential
	var created, updated string
	err := r.DB.QueryRowContext(ctx, `
SELECT id,name,credential_type,COALESCE(username,''),encrypted_secret,COALESCE(metadata_json,'{}'),created_at,updated_at
FROM credentials WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.CredentialType, &c.Username, &c.EncryptedSecret, &c.MetadataJSON, &created, &updated)
	if err == nil {
		c.CreatedAt, _ = time.Parse(time.RFC3339, created)
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	}
	return c, err
}

func (r CredentialRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx, "DELETE FROM credentials WHERE id=?", id)
	return err
}
