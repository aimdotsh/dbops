package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestServerIDsConcurrentAndIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ids.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	_, err = db.Exec(`CREATE TABLE mysql_server_ids(id INTEGER PRIMARY KEY,server_id INTEGER UNIQUE NOT NULL,host_id INTEGER NOT NULL,port INTEGER NOT NULL,task_id INTEGER,instance_id INTEGER,status TEXT,created_at TEXT,UNIQUE(host_id,port))`)
	if err != nil {
		t.Fatal(err)
	}
	repo := ServerIDRepo{DB: db}
	var wg sync.WaitGroup
	for n := 0; n < 64; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			host := int64(n%16 + 1)
			r, e := repo.Reserve(context.Background(), int64(n+1), host, 13307)
			if e != nil {
				t.Error(e)
				return
			}
			if r.HostID != host || r.ServerID < 1001 {
				t.Errorf("wrong reservation: %+v", r)
			}
		}(n)
	}
	wg.Wait()
	var count, distinct int
	if err = db.QueryRow("SELECT count(*),count(DISTINCT server_id) FROM mysql_server_ids").Scan(&count, &distinct); err != nil {
		t.Fatal(err)
	}
	if count != 16 || distinct != 16 {
		t.Fatalf("duplicate/lost allocations: %d %d", count, distinct)
	}
	_, err = db.Exec("INSERT INTO mysql_server_ids(server_id,host_id,port,status,created_at) VALUES(4294967295,100,1,'bound','')")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Reserve(context.Background(), 200, 1, 13307); err != nil {
		t.Fatalf("existing reservation lost at exhaustion: %v", err)
	}
	if _, err = repo.Reserve(context.Background(), 200, 200, 13307); err == nil {
		t.Fatal("exhaustion not rejected")
	}
}
