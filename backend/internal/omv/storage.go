package omv

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Caller invokes one OMV RPC as the current user. Adapters below depend on it instead of
// Client so they are decoupled from sessions.
type Caller func(ctx context.Context, service, method string, params, out any) error

// flexUint decodes numbers that OMV sometimes sends as JSON strings.
type flexUint uint64

func (f *flexUint) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if v, err := strconv.ParseUint(s, 10, 64); err == nil {
		*f = flexUint(v)
		return nil
	}
	v, err := strconv.ParseFloat(s, 64) // tolerate "1.5e+12"
	if err != nil {
		return err
	}
	*f = flexUint(v)
	return nil
}

// Filesystem is a mounted filesystem as reported by OMV.
type Filesystem struct {
	UUID       string  `json:"uuid"`
	Device     string  `json:"device"`
	Label      string  `json:"label"`
	Type       string  `json:"type"`
	MountPoint string  `json:"mountPoint"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
	// MntEntRef is the mount-entry id ShareMgmt.set needs (not the filesystem uuid); empty if unknown.
	MntEntRef string `json:"mntEntRef,omitempty"`
	canonical string
}

// ListFilesystems wraps FileSystemMgmt.enumerateFilesystems and returns mounted filesystems.
// ⚠️ a validar contra OMV 7: field names (devicefile, uuid, label, type, mountpoint, mounted,
// size, available, used).
func ListFilesystems(ctx context.Context, call Caller) ([]Filesystem, error) {
	var raw []struct {
		UUID       string   `json:"uuid"`
		DeviceFile string   `json:"devicefile"`
		Canonical  string   `json:"canonicaldevicefile"`
		Label      string   `json:"label"`
		Type       string   `json:"type"`
		MountPoint string   `json:"mountpoint"`
		Mounted    bool     `json:"mounted"`
		Size       flexUint `json:"size"`
		Available  flexUint `json:"available"`
	}
	if err := call(ctx, "FileSystemMgmt", "enumerateFilesystems", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Filesystem, 0, len(raw))
	for _, r := range raw {
		if !r.Mounted || r.MountPoint == "" {
			continue
		}
		fs := Filesystem{
			UUID: r.UUID, Device: r.DeviceFile, Label: r.Label, Type: r.Type, MountPoint: r.MountPoint, canonical: r.Canonical,
			Total: uint64(r.Size), Free: uint64(r.Available),
		}
		if fs.Total >= fs.Free { // OMV sends "used" as text such as "1.67 TiB"; derive it instead
			fs.Used = fs.Total - fs.Free
		}
		if fs.Total > 0 {
			fs.Percent = float64(int(float64(fs.Used)/float64(fs.Total)*1000+0.5)) / 10
		}
		out = append(out, fs)
	}
	return out, nil
}

// OMVVersion returns the OMV version via System.getInformation, or "" if unavailable.
// ⚠️ a validar contra OMV 7 (field "version").
func OMVVersion(ctx context.Context, call Caller) string {
	var info struct {
		Version string `json:"version"`
	}
	if err := call(ctx, "System", "getInformation", nil, &info); err != nil {
		return ""
	}
	return info.Version
}

// RegisterStorageFixtures installs demo answers for the storage-related RPCs on a Mock.
func RegisterStorageFixtures(m *Mock) {
	const gib = uint64(1) << 30
	m.Handle("FileSystemMgmt.enumerateFilesystems", func(json.RawMessage) (any, error) {
		return []map[string]any{
			{"uuid": "d1f5c3a0-0000-4000-8000-0000000000a1", "devicefile": "/dev/disk/by-uuid/d1f5c3a0", "canonicaldevicefile": "/dev/sda1", "label": "data",
				"type": "ext4", "mountpoint": "/srv/dev-disk-by-uuid-1111", "mounted": true,
				"size": strconv.FormatUint(2000*gib, 10), "available": strconv.FormatUint(1240*gib, 10), "used": "760.00 GiB"},
			{"uuid": "d1f5c3a0-0000-4000-8000-0000000000b2", "devicefile": "/dev/disk/by-uuid/d1f5c3b0", "canonicaldevicefile": "/dev/sdb1", "label": "backup",
				"type": "ext4", "mountpoint": "/srv/dev-disk-by-uuid-2222", "mounted": true,
				"size": strconv.FormatUint(4000*gib, 10), "available": strconv.FormatUint(300*gib, 10), "used": "3.61 TiB"},
			{"uuid": "33333333-cccc-4ccc-8ccc-333333333333", "devicefile": "/dev/sdc1", "label": "",
				"type": "ext4", "mountpoint": "", "mounted": false, "size": "0", "available": "0", "used": "0"},
		}, nil
	})
	m.Handle("System.getInformation", func(json.RawMessage) (any, error) {
		return map[string]any{"version": "7.4.17-1 (Sandworm)"}, nil
	})
}

// MountCandidate is one entry of ShareMgmt.getCandidates: the mount-entry id plus a description
// that starts with the device, e.g. "/dev/sda1 [NTFS, 1.67 TiB (93%) used, ...]".
type MountCandidate struct {
	UUID        string `json:"uuid"`
	Description string `json:"description"`
}

// ListFilesystemsWithRefs is ListFilesystems plus the mntentref of each filesystem, found by
// matching ShareMgmt.getCandidates descriptions against the canonical device. Filesystems OMV
// does not offer as share locations keep an empty MntEntRef.
func ListFilesystemsWithRefs(ctx context.Context, call Caller) ([]Filesystem, error) {
	fss, err := ListFilesystems(ctx, call)
	if err != nil {
		return nil, err
	}
	var cands []MountCandidate
	if err := call(ctx, "ShareMgmt", "getCandidates", nil, &cands); err != nil {
		return fss, nil // listing still works; creating shares will not offer these disks
	}
	for i := range fss {
		if fss[i].canonical == "" {
			continue
		}
		for _, c := range cands {
			if strings.HasPrefix(c.Description, fss[i].canonical+" [") {
				fss[i].MntEntRef = c.UUID
			}
		}
	}
	return fss, nil
}
