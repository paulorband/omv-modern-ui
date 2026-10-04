// Package storage orchestrates shared-folder operations on top of the OMV RPC adapters (SPEC-06).
package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"sync"

	"omvhome/internal/omv"
)

var (
	nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	relRe  = regexp.MustCompile(`^[a-zA-Z0-9_./ -]+$`)
)

// InvalidError is bad input (HTTP 400); ConflictError a clash (409); ErrNotFound a missing share (404).
type (
	InvalidError  struct{ Msg string }
	ConflictError struct{ Msg string }
)

func (e *InvalidError) Error() string  { return e.Msg }
func (e *ConflictError) Error() string { return e.Msg }

var ErrNotFound = errors.New("compartilhamento não encontrado")

// StepError says which OMV step failed (R06.4).
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("falha na etapa %q: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

func step(name string, err error) error {
	if err == nil {
		return nil
	}
	return &StepError{Step: name, Err: err}
}

// ValidUUID reports whether s looks like an OMV object uuid.
func ValidUUID(s string) bool { return uuidRe.MatchString(s) }

type Share struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	FilesystemUUID string `json:"filesystemUuid"`
	Comment        string `json:"comment"`
	SMB            bool   `json:"smb"`
	SMBGuest       bool   `json:"smbGuest"`
	NFS            bool   `json:"nfs"`
}

type CreateInput struct {
	Name           string `json:"name"`
	FilesystemUUID string `json:"filesystemUuid"`
	RelativePath   string `json:"relativePath"`
	Comment        string `json:"comment"`
	SMB            bool   `json:"smb"`
	SMBGuest       bool   `json:"smbGuest"`
}

type UpdateInput struct {
	SMB      *bool `json:"smb"`
	SMBGuest *bool `json:"smbGuest"`
}

// Service serialises mutations: OMV keeps one global pending configuration.
type Service struct{ mu sync.Mutex }

// List returns shared folders joined with their SMB/NFS exports and resolved paths.
func (s *Service) List(ctx context.Context, call omv.Caller) ([]Share, error) {
	folders, err := omv.ListSharedFolders(ctx, call)
	if err != nil {
		return nil, err
	}
	// SMB/NFS may be disabled or not installed: absence is not an error.
	smb, _ := omv.ListSMBShares(ctx, call)
	nfs, _ := omv.ListNFSShares(ctx, call)
	mounts := map[string]string{}
	if fss, err := omv.ListFilesystems(ctx, call); err == nil {
		for _, f := range fss {
			mounts[f.UUID] = f.MountPoint
		}
	}
	out := make([]Share, 0, len(folders))
	for _, f := range folders {
		sh := Share{UUID: f.UUID, Name: f.Name, FilesystemUUID: f.MntEntRef, Comment: f.Comment}
		if mp, ok := mounts[f.MntEntRef]; ok {
			sh.Path = path.Join(mp, f.RelDirPath)
		}
		for _, x := range smb {
			if x.SharedFolderRef == f.UUID && x.Enable {
				sh.SMB, sh.SMBGuest = true, x.Guest != "" && x.Guest != "no"
			}
		}
		for _, x := range nfs {
			if x.SharedFolderRef == f.UUID && x.Enable {
				sh.NFS = true
			}
		}
		out = append(out, sh)
	}
	return out, nil
}

func (s *Service) find(ctx context.Context, call omv.Caller, uuid string) (*Share, error) {
	list, err := s.List(ctx, call)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].UUID == uuid {
			return &list[i], nil
		}
	}
	return nil, ErrNotFound
}

// cleanRelPath validates a path relative to the filesystem root and returns it with a trailing slash.
func cleanRelPath(p string) (string, error) {
	if strings.HasPrefix(p, "/") || !relRe.MatchString(p) {
		return "", &InvalidError{"caminho relativo inválido"}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", &InvalidError{"o caminho não pode conter .."}
		}
	}
	c := path.Clean(p)
	if c == "." {
		return "", &InvalidError{"caminho relativo inválido"}
	}
	return c + "/", nil
}

// Validate normalises and checks create input (A06.3: runs before any OMV call).
func (in *CreateInput) Validate() error {
	if !nameRe.MatchString(in.Name) {
		return &InvalidError{"nome inválido: use 1 a 32 letras, números, _ ou -"}
	}
	if !ValidUUID(in.FilesystemUUID) {
		return &InvalidError{"filesystem inválido"}
	}
	if in.RelativePath == "" {
		in.RelativePath = in.Name
	}
	rel, err := cleanRelPath(in.RelativePath)
	if err != nil {
		return err
	}
	in.RelativePath = rel
	if len(in.Comment) > 200 || strings.ContainsAny(in.Comment, "\n\r\x00") {
		return &InvalidError{"comentário inválido"}
	}
	if in.SMBGuest && !in.SMB {
		return &InvalidError{"acesso convidado exige SMB ativo"}
	}
	return nil
}

// Create makes a shared folder (and optionally its SMB export), rolling back on failure (R06.4).
func (s *Service) Create(ctx context.Context, call omv.Caller, in CreateInput) (*Share, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := omv.ListSharedFolders(ctx, call)
	if err != nil {
		return nil, step("listar compartilhamentos", err)
	}
	for _, f := range existing {
		if strings.EqualFold(f.Name, in.Name) {
			return nil, &ConflictError{fmt.Sprintf("já existe um compartilhamento chamado %q", in.Name)}
		}
	}

	folderUUID, err := omv.CreateSharedFolder(ctx, call, in.Name, in.FilesystemUUID, in.RelativePath, in.Comment)
	if err != nil {
		return nil, step("criar pasta compartilhada", err)
	}
	rollback := func(smbUUID string) {
		bg := context.WithoutCancel(ctx)
		if smbUUID != "" {
			if err := omv.DeleteSMBShare(bg, call, smbUUID); err != nil {
				slog.Warn("rollback: falha ao remover SMB", "err", err)
			}
		}
		if err := omv.DeleteSharedFolder(bg, call, folderUUID); err != nil {
			slog.Warn("rollback: falha ao remover pasta", "err", err)
		}
	}

	smbUUID := ""
	if in.SMB {
		smbUUID, err = omv.SetSMBShare(ctx, call, omv.NewObjectUUID, folderUUID, in.Name, in.Comment, in.SMBGuest)
		if err != nil {
			rollback("")
			return nil, step("criar compartilhamento SMB", err)
		}
	}
	if err := omv.ApplyChanges(ctx, call); err != nil {
		rollback(smbUUID)
		return nil, step("aplicar alterações", err)
	}
	return s.find(ctx, call, folderUUID)
}

// Delete removes a shared folder and its exports; data on disk is never touched (R06.5).
func (s *Service) Delete(ctx context.Context, call omv.Caller, uuid string) error {
	if !ValidUUID(uuid) {
		return &InvalidError{"identificador inválido"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.find(ctx, call, uuid); err != nil {
		return err
	}
	smb, _ := omv.ListSMBShares(ctx, call)
	nfs, _ := omv.ListNFSShares(ctx, call)
	for _, x := range smb {
		if x.SharedFolderRef == uuid {
			if err := omv.DeleteSMBShare(ctx, call, x.UUID); err != nil {
				return step("remover SMB", err)
			}
		}
	}
	for _, x := range nfs {
		if x.SharedFolderRef == uuid {
			if err := omv.DeleteNFSShare(ctx, call, x.UUID); err != nil {
				return step("remover NFS", err)
			}
		}
	}
	if err := omv.DeleteSharedFolder(ctx, call, uuid); err != nil {
		return step("remover pasta compartilhada", err)
	}
	return step("aplicar alterações", omv.ApplyChanges(ctx, call))
}

// Update toggles SMB and guest access (R06.6).
func (s *Service) Update(ctx context.Context, call omv.Caller, uuid string, in UpdateInput) (*Share, error) {
	if !ValidUUID(uuid) {
		return nil, &InvalidError{"identificador inválido"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.find(ctx, call, uuid)
	if err != nil {
		return nil, err
	}
	wantSMB, wantGuest := cur.SMB, cur.SMBGuest
	if in.SMB != nil {
		wantSMB = *in.SMB
	}
	if in.SMBGuest != nil {
		wantGuest = *in.SMBGuest
	}
	if !wantSMB && in.SMBGuest != nil && *in.SMBGuest {
		return nil, &InvalidError{"acesso convidado exige SMB ativo"}
	}
	if !wantSMB {
		wantGuest = false
	}
	if wantSMB == cur.SMB && wantGuest == cur.SMBGuest {
		return cur, nil
	}

	smb, _ := omv.ListSMBShares(ctx, call)
	existing := ""
	for _, x := range smb {
		if x.SharedFolderRef == uuid {
			existing = x.UUID
		}
	}
	switch {
	case wantSMB:
		target := existing
		if target == "" {
			target = omv.NewObjectUUID
		}
		if _, err := omv.SetSMBShare(ctx, call, target, uuid, cur.Name, cur.Comment, wantGuest); err != nil {
			return nil, step("configurar SMB", err)
		}
	case existing != "":
		if err := omv.DeleteSMBShare(ctx, call, existing); err != nil {
			return nil, step("remover SMB", err)
		}
	}
	if err := omv.ApplyChanges(ctx, call); err != nil {
		return nil, step("aplicar alterações", err)
	}
	return s.find(ctx, call, uuid)
}
