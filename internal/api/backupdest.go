package api

import (
	"encoding/json"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Offsite backup destinations (admin only). A destination is an rclone remote;
// completed account backups are uploaded to every enabled destination and pruned
// to its retention. Secrets are stored in the config but never returned.

// destRequest is the create/update payload. Fields are the per-type inputs (e.g.
// access keys, host/user/pass); the handler turns them into rclone parameters.
type destRequest struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	RemotePath string            `json:"remote_path"`
	Keep       int               `json:"keep"`
	Enabled    bool              `json:"enabled"`
	Fields     map[string]string `json:"fields"`
}

func (s *Server) handleDestList(w http.ResponseWriter, r *http.Request, u *models.User) {
	rows, err := s.DB.Query(`SELECT id, name, type, remote_path, enabled, keep, created_at FROM backup_destinations ORDER BY name`)
	if err != nil {
		s.fail(w, "list destinations", err)
		return
	}
	defer rows.Close()
	out := []models.BackupDestination{} // never includes secrets
	for rows.Next() {
		var d models.BackupDestination
		var en int
		if rows.Scan(&d.ID, &d.Name, &d.Type, &d.RemotePath, &en, &d.Keep, &d.CreatedAt) == nil {
			d.Enabled = en != 0
			out = append(out, d)
		}
	}
	s.json(w, map[string]any{"destinations": out, "rclone": system.HaveRclone()})
}

// rcloneParams turns the typed fields into rclone config parameters, obscuring
// passwords where rclone requires it.
func (s *Server) rcloneParams(typ string, f map[string]string) (map[string]string, error) {
	get := func(k string) string { return strings.TrimSpace(f[k]) }
	switch typ {
	case "s3":
		p := map[string]string{"type": "s3", "access_key_id": get("access_key_id"), "secret_access_key": get("secret_access_key")}
		p["provider"] = "Other"
		if get("endpoint") == "" {
			p["provider"] = "AWS"
		} else {
			p["endpoint"] = get("endpoint")
		}
		if r := get("region"); r != "" {
			p["region"] = r
		}
		return p, nil
	case "b2":
		return map[string]string{"type": "b2", "account": get("account"), "key": get("key")}, nil
	case "sftp", "ftp":
		obscured, err := system.RcloneObscure(get("pass"))
		if err != nil {
			return nil, err
		}
		p := map[string]string{"type": typ, "host": get("host"), "user": get("user"), "pass": obscured}
		if port := get("port"); port != "" {
			p["port"] = port
		}
		return p, nil
	case "rclone":
		return map[string]string{"raw": f["raw"]}, nil
	}
	return nil, errBadType
}

var errBadType = &apiError{"type must be s3, b2, sftp, ftp or rclone"}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

func (s *Server) handleDestCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[destRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.err(w, http.StatusBadRequest, "a name is required")
		return
	}
	params, err := s.rcloneParams(req.Type, req.Fields)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, _ := json.Marshal(params)
	keep := req.Keep
	if keep <= 0 {
		keep = 7
	}
	res, err := s.DB.Exec(`INSERT INTO backup_destinations(name,type,config,remote_path,enabled,keep)
		VALUES(?,?,?,?,?,?)`, req.Name, req.Type, string(cfg), strings.TrimSpace(req.RemotePath), boolInt(req.Enabled), keep)
	if err != nil {
		s.fail(w, "create destination", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleDestUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	req, err := decode[destRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	keep := req.Keep
	if keep <= 0 {
		keep = 7
	}
	// Rebuild the config only when new secret fields were supplied.
	if len(req.Fields) > 0 {
		params, err := s.rcloneParams(req.Type, req.Fields)
		if err != nil {
			s.err(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg, _ := json.Marshal(params)
		s.DB.Exec(`UPDATE backup_destinations SET config = ? WHERE id = ?`, string(cfg), id)
	}
	if _, err := s.DB.Exec(`UPDATE backup_destinations SET name=?, remote_path=?, enabled=?, keep=? WHERE id=?`,
		req.Name, strings.TrimSpace(req.RemotePath), boolInt(req.Enabled), keep, id); err != nil {
		s.fail(w, "update destination", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleDestDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	if _, err := s.DB.Exec(`DELETE FROM backup_destinations WHERE id = ?`, pathID(r, "id")); err != nil {
		s.fail(w, "delete destination", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleDestTest(w http.ResponseWriter, r *http.Request, u *models.User) {
	cfg, remotePath, ok := s.destConfig(pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "destination not found")
		return
	}
	if err := system.RcloneTest(cfg, remotePath); err != nil {
		s.err(w, http.StatusBadGateway, "connection failed: "+err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// destConfig loads a destination's rclone params and remote path.
func (s *Server) destConfig(id int64) (map[string]string, string, bool) {
	var cfgJSON, remotePath string
	if s.DB.QueryRow(`SELECT config, remote_path FROM backup_destinations WHERE id = ?`, id).
		Scan(&cfgJSON, &remotePath) != nil {
		return nil, "", false
	}
	var cfg map[string]string
	if json.Unmarshal([]byte(cfgJSON), &cfg) != nil {
		return nil, "", false
	}
	return cfg, remotePath, true
}

// uploadToDestinations sends a completed backup to every enabled destination and
// prunes each to its retention. Best-effort: a failing destination is logged, not
// fatal to the backup.
func (s *Server) uploadToDestinations(filename, username string) {
	rows, err := s.DB.Query(`SELECT config, remote_path, keep FROM backup_destinations WHERE enabled = 1`)
	if err != nil {
		return
	}
	type dest struct {
		cfg        map[string]string
		remotePath string
		keep       int
	}
	var dests []dest
	for rows.Next() {
		var cfgJSON, remotePath string
		var keep int
		if rows.Scan(&cfgJSON, &remotePath, &keep) == nil {
			var cfg map[string]string
			if json.Unmarshal([]byte(cfgJSON), &cfg) == nil {
				dests = append(dests, dest{cfg, remotePath, keep})
			}
		}
	}
	rows.Close()
	if len(dests) == 0 {
		return
	}
	local := filepath.Join(s.backupsDir(), filename)
	for _, d := range dests {
		if err := system.RcloneUpload(d.cfg, d.remotePath, filename, local); err != nil {
			log.Printf("offsite upload of %s failed: %v", filename, err)
			continue
		}
		system.RclonePrune(d.cfg, path.Join(d.remotePath, username), d.keep)
	}
}

// handleRcloneInstall installs rclone in the background (admin).
func (s *Server) handleRcloneInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	go func() {
		if err := system.InstallRclone(); err != nil {
			log.Printf("install rclone: %v", err)
		}
	}()
	s.json(w, map[string]bool{"ok": true})
}
