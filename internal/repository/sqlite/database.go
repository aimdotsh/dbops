package sqlite

import (
	"context"
	"database/sql"

	"github.com/aimdotsh/dbops/internal/domain"
)

type DatabaseRepo struct{ DB *sql.DB }

func (r DatabaseRepo) List(ctx context.Context) ([]domain.DatabaseInstance, error) {
	rows, err := r.DB.QueryContext(ctx, "SELECT id,name,db_type,COALESCE(version,''),COALESCE(host_id,0),COALESCE(port,0),COALESCE(role,''),status,managed_mode FROM database_instances ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DatabaseInstance
	for rows.Next() {
		var d domain.DatabaseInstance
		if err := rows.Scan(&d.ID, &d.Name, &d.DBType, &d.Version, &d.HostID, &d.Port, &d.Role, &d.Status, &d.ManagedMode); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
