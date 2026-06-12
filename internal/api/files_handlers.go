package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// jailFor returns the directory a user's file manager is confined to:
// customers get their own web space, admins the whole web root.
func (s *Server) jailFor(u *models.User) (jail, sysUser string, err error) {
	if u.Role == models.RoleAdmin {
		return s.Cfg.WebRoot, "", nil
	}
	sysUser, err = s.sysUserForPanelUser(u.ID)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(s.Cfg.WebRoot, sysUser), sysUser, nil
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, _, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	rel := r.URL.Query().Get("path")
	entries, err := system.ListDir(jail, rel)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]any{"path": path.Clean("/" + rel), "entries": entries})
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, _, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	data, err := system.ReadFileJailed(jail, r.URL.Query().Get("path"))
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]string{"content": string(data)})
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, sysUser, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	req, err := decode[struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WriteFileJailed(jail, req.Path, []byte(req.Content), sysUser); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, _, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	rel := r.URL.Query().Get("path")
	f, st, err := system.OpenFileJailed(jail, rel)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	defer f.Close()
	name := filepath.Base(rel)
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	io.Copy(w, f)
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request, u *models.User) {
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	jail, sysUser, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		s.err(w, http.StatusBadRequest, "invalid upload")
		return
	}
	dir := r.FormValue("path")
	file, header, err := r.FormFile("file")
	if err != nil {
		s.err(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	dest := path.Join(dir, filepath.Base(header.Filename))
	if err := system.SaveUploadJailed(jail, dest, file, sysUser); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, sysUser, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	req, err := decode[struct {
		Path string `json:"path"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.MkdirJailed(jail, req.Path, sysUser); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, _, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	req, err := decode[struct {
		From string `json:"from"`
		To   string `json:"to"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.RenameJailed(jail, req.From, req.To); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	jail, _, err := s.jailFor(u)
	if err != nil {
		s.fail(w, "resolve jail", err)
		return
	}
	req, err := decode[struct {
		Path string `json:"path"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.DeleteJailed(jail, req.Path); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
