package store

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// dataOwner says that a data folder inside APPDATA must belong to a numeric uid:gid (SPEC-09 R09.10).
type dataOwner struct {
	path     string
	uid, gid int // -1 leaves that id unchanged
}

// parseUser reads a compose `user:` value ("1000", "1000:100", 1000). Names are not resolved.
func parseUser(v any) (uid, gid int, ok bool) {
	var s string
	switch t := v.(type) {
	case int:
		s = strconv.Itoa(t)
	case string:
		s = strings.TrimSpace(t)
	default:
		return 0, 0, false
	}
	u, g, hasGroup := strings.Cut(s, ":")
	uid, err := strconv.Atoi(u)
	if err != nil || uid < 0 {
		return 0, 0, false
	}
	gid = -1
	if hasGroup {
		if n, err := strconv.Atoi(g); err == nil && n >= 0 {
			gid = n
		}
	}
	return uid, gid, true
}

// appDataOwners lists the bind-mounted folders under appData whose service runs as a numeric user.
// Services without a numeric `user:` are left alone: their images fix permissions themselves.
func appDataOwners(rendered []byte, appData string) []dataOwner {
	return appDataOwnersFor(rendered, appData, nil)
}

// appDataOwnersFor is appDataOwners plus, when imageUser is given, the image's own USER for services
// that set no `user:` (SPEC-09 R09.12). Images that run as root report ok=false and are left alone.
func appDataOwnersFor(rendered []byte, appData string, imageUser func(image string) (uid, gid int, ok bool)) []dataOwner {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if yaml.Unmarshal(rendered, &doc) != nil {
		return nil
	}
	appData = filepath.Clean(appData)
	byPath := map[string]dataOwner{}
	names := make([]string, 0, len(doc.Services))
	for n := range doc.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		svc := doc.Services[n]
		uid, gid, ok := parseUser(svc["user"])
		if !ok && imageUser != nil && svc["user"] == nil {
			if image, _ := svc["image"].(string); image != "" && !strings.Contains(image, "$") {
				uid, gid, ok = imageUser(strings.TrimSpace(image))
			}
		}
		if !ok {
			continue
		}
		vols, _ := svc["volumes"].([]any)
		for _, v := range vols {
			var src string
			switch vv := v.(type) {
			case string:
				src, _, _ = strings.Cut(vv, ":")
			case map[string]any:
				if t, _ := vv["type"].(string); t == "" || t == "bind" {
					src, _ = vv["source"].(string)
				}
			}
			p, ok := resolveInAppData(src, appData)
			if !ok {
				continue
			}
			if _, dup := byPath[p]; !dup { // first service (by name) wins
				byPath[p] = dataOwner{path: p, uid: uid, gid: gid}
			}
		}
	}
	out := make([]dataOwner, 0, len(byPath))
	for _, o := range byPath {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// resolveInAppData expands ${APPDATA} in a volume source and keeps it only if it stays inside appData.
func resolveInAppData(src, appData string) (string, bool) {
	for _, ref := range []string{"${APPDATA}", "$APPDATA"} {
		if strings.HasPrefix(src, ref) {
			src = appData + strings.TrimPrefix(src, ref)
			break
		}
	}
	if strings.Contains(src, "$") || !filepath.IsAbs(src) {
		return "", false
	}
	p := filepath.Clean(src)
	if p != appData && !strings.HasPrefix(p, appData+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// prepareOwners creates each data folder (so Docker does not create it as root) and hands it to
// the user the container runs as. Failures only warn: running without root cannot chown.
func (i *Installer) prepareOwners(owners []dataOwner) {
	for _, o := range owners {
		// a volume source that looks like a file (has an extension) is left for the app to create
		if filepath.Ext(o.path) != "" {
			continue
		}
		if err := i.MkdirAll(o.path); err != nil {
			slog.Warn("não foi possível criar a pasta de dados", "path", o.path, "err", err)
			continue
		}
		var firstErr error
		_ = filepath.WalkDir(o.path, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if cerr := i.Chown(p, o.uid, o.gid); cerr != nil && firstErr == nil && !os.IsNotExist(cerr) {
				firstErr = cerr
			}
			return nil
		})
		if firstErr != nil {
			slog.Warn("não foi possível ajustar o dono da pasta de dados", "path", o.path, "uid", o.uid, "gid", o.gid, "err", firstErr)
		}
	}
}
