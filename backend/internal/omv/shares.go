package omv

import (
	"context"
	"fmt"
	"time"
)

// ⚠️ Everything in this file encodes assumptions about the OMV 7 RPC API (method names,
// parameter and field names) that have NOT been validated against a real instance yet.
// Validate with: omv-rpc -u admin ShareMgmt getList '{"start":0,"limit":-1}' and friends.

// NewObjectUUID is the placeholder OMV uses to ask for a new configuration object.
const NewObjectUUID = "fa4b1c66-ef79-11e5-87a0-0002b3a176b4"

type listParams struct {
	Start int `json:"start"`
	Limit int `json:"limit"`
}

var allRows = listParams{Start: 0, Limit: -1}

// SharedFolder is an OMV shared folder.
type SharedFolder struct {
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	MntEntRef  string `json:"mntentref"`
	RelDirPath string `json:"reldirpath"`
	AbsDirPath string `json:"absdirpath"`
	Comment    string `json:"comment"`
}

// ServiceShare is an SMB or NFS export of a shared folder.
type ServiceShare struct {
	UUID            string `json:"uuid"`
	SharedFolderRef string `json:"sharedfolderref"`
	Enable          bool   `json:"enable"`
	Guest           string `json:"guest"`
}

// ListSharedFolders wraps ShareMgmt.getList.
func ListSharedFolders(ctx context.Context, call Caller) ([]SharedFolder, error) {
	var out struct {
		Data []SharedFolder `json:"data"`
	}
	if err := call(ctx, "ShareMgmt", "getList", allRows, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ListSMBShares wraps SMB.getShareList. A failure means SMB is not available: callers treat it as "none".
func ListSMBShares(ctx context.Context, call Caller) ([]ServiceShare, error) {
	var out struct {
		Data []ServiceShare `json:"data"`
	}
	if err := call(ctx, "SMB", "getShareList", allRows, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ListNFSShares wraps NFS.getShareList (same semantics as ListSMBShares).
func ListNFSShares(ctx context.Context, call Caller) ([]ServiceShare, error) {
	var out struct {
		Data []ServiceShare `json:"data"`
	}
	if err := call(ctx, "NFS", "getShareList", allRows, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateSharedFolder wraps ShareMgmt.set for a new folder and returns its uuid.
func CreateSharedFolder(ctx context.Context, call Caller, name, mntEntRef, relDirPath, comment string) (string, error) {
	params := map[string]any{
		"uuid": NewObjectUUID, "name": name, "mntentref": mntEntRef, "reldirpath": relDirPath,
		"comment": comment, "mode": "775",
	}
	var out struct {
		UUID string `json:"uuid"`
	}
	if err := call(ctx, "ShareMgmt", "set", params, &out); err != nil {
		return "", err
	}
	if out.UUID == "" {
		return "", fmt.Errorf("omv: ShareMgmt.set não retornou uuid")
	}
	return out.UUID, nil
}

// DeleteSharedFolder wraps ShareMgmt.delete. recursive is always false: data on disk is kept.
func DeleteSharedFolder(ctx context.Context, call Caller, uuid string) error {
	return call(ctx, "ShareMgmt", "delete", map[string]any{"uuid": uuid, "recursive": false}, nil)
}

// SetSMBShare wraps SMB.setShare. uuid = NewObjectUUID creates; an existing uuid updates.
// guest: "no" or "allow" (value seen on a real OMV share).
func SetSMBShare(ctx context.Context, call Caller, uuid, folderUUID, name, comment string, guest bool) (string, error) {
	g := "no"
	if guest {
		g = "allow"
	}
	params := map[string]any{
		"uuid": uuid, "sharedfolderref": folderUUID, "name": name, "comment": comment,
		"enable": true, "guest": g, "readonly": false, "browseable": true,
		"inheritacls": true, "inheritpermissions": true, "recyclebin": false, "hidedotfiles": true,
	}
	var out struct {
		UUID string `json:"uuid"`
	}
	if err := call(ctx, "SMB", "setShare", params, &out); err != nil {
		return "", err
	}
	return out.UUID, nil
}

func DeleteSMBShare(ctx context.Context, call Caller, uuid string) error {
	return call(ctx, "SMB", "deleteShare", map[string]any{"uuid": uuid}, nil)
}

func DeleteNFSShare(ctx context.Context, call Caller, uuid string) error {
	return call(ctx, "NFS", "deleteShare", map[string]any{"uuid": uuid}, nil)
}

// User is an OMV user (read-only in the MVP).
type User struct {
	Name    string `json:"name"`
	UID     int    `json:"uid"`
	Comment string `json:"comment"`
}

// ListUsers wraps UserMgmt.enumerateUsers ⚠️.
func ListUsers(ctx context.Context, call Caller) ([]User, error) {
	var out []User
	if err := call(ctx, "UserMgmt", "enumerateUsers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ApplyTimeout bounds how long ApplyChanges waits for OMV to deploy the configuration (R06.8).
var ApplyTimeout = 60 * time.Second

// ApplyPollInterval is how often the background job is polled.
var ApplyPollInterval = 500 * time.Millisecond

// ApplyChanges deploys pending configuration: Config.applyChangesBg returns the name of a
// background-job file, polled with Exec.isRunning until it finishes ⚠️.
func ApplyChanges(ctx context.Context, call Caller) error {
	var job string
	if err := call(ctx, "Config", "applyChangesBg", map[string]any{"modules": []string{}, "force": false}, &job); err != nil {
		return err
	}
	if job == "" {
		return nil // OMV applied synchronously
	}
	ctx, cancel := context.WithTimeout(ctx, ApplyTimeout)
	defer cancel()
	for {
		var running bool
		if err := call(ctx, "Exec", "isRunning", map[string]any{"filename": job}, &running); err != nil {
			return err
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("omv: tempo esgotado aplicando as alterações")
		case <-time.After(ApplyPollInterval):
		}
	}
}
