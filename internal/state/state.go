package state

import (
	"database/sql"
	"encoding/json"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"time"
)

type Run struct {
	ID         string    `json:"id"`
	Recipe     string    `json:"recipe"`
	Phase      string    `json:"phase"`
	ProviderID string    `json:"provider_id,omitempty"`
	OutputDir  string    `json:"output_dir,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type Store struct{ DB *sql.DB }

func Path() string {
	if p := os.Getenv("GPU_RUN_STATE_DIR"); p != "" {
		return filepath.Join(p, "runs.sqlite")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "gpu-run", "runs.sqlite")
}
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, e := sql.Open("sqlite3", path)
	if e != nil {
		return nil, e
	}
	if _, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, recipe TEXT NOT NULL, phase TEXT NOT NULL, provider_id TEXT, output_dir TEXT, error TEXT, created_at TEXT, updated_at TEXT)`); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}
func (s *Store) Put(r Run) error {
	_, e := s.DB.Exec(`INSERT INTO runs(id,recipe,phase,provider_id,output_dir,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET phase=excluded.phase,provider_id=excluded.provider_id,output_dir=excluded.output_dir,error=excluded.error,updated_at=excluded.updated_at`, r.ID, r.Recipe, r.Phase, r.ProviderID, r.OutputDir, r.Error, r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	return e
}
func (s *Store) Get(id string) (Run, error) {
	var r Run
	var c, u string
	e := s.DB.QueryRow(`SELECT id,recipe,phase,provider_id,output_dir,error,created_at,updated_at FROM runs WHERE id=?`, id).Scan(&r.ID, &r.Recipe, &r.Phase, &r.ProviderID, &r.OutputDir, &r.Error, &c, &u)
	if e != nil {
		return r, e
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
	return r, nil
}
func (s *Store) List() ([]Run, error) {
	rows, e := s.DB.Query(`SELECT id,recipe,phase,provider_id,output_dir,error,created_at,updated_at FROM runs ORDER BY created_at DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var c, u string
		if e := rows.Scan(&r.ID, &r.Recipe, &r.Phase, &r.ProviderID, &r.OutputDir, &r.Error, &c, &u); e != nil {
			return nil, e
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
		out = append(out, r)
	}
	return out, rows.Err()
}
func (r Run) JSON() string { b, _ := json.Marshal(r); return string(b) }
