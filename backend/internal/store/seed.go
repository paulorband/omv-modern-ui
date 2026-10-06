package store

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// seedMount is a bind mount of an APPDATA folder over a directory the image may fill (SPEC-09 R09.11).
type seedMount struct {
	image         string
	containerPath string
	hostDir       string
}

// appDataSeeds lists the bind mounts under appData that cover a directory (not a file) of a service
// image. Images that depend on variables are skipped: they cannot be resolved here.
func appDataSeeds(rendered []byte, appData string) []seedMount {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if yaml.Unmarshal(rendered, &doc) != nil {
		return nil
	}
	appData = filepath.Clean(appData)
	names := make([]string, 0, len(doc.Services))
	for n := range doc.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []seedMount
	seen := map[string]bool{}
	for _, n := range names {
		svc := doc.Services[n]
		image, _ := svc["image"].(string)
		image = strings.TrimSpace(image)
		if image == "" || strings.Contains(image, "$") || strings.HasPrefix(image, "-") {
			continue
		}
		vols, _ := svc["volumes"].([]any)
		for _, v := range vols {
			var src, dst string
			switch vv := v.(type) {
			case string:
				parts := strings.SplitN(vv, ":", 3)
				if len(parts) < 2 {
					continue
				}
				src, dst = parts[0], parts[1]
			case map[string]any:
				if t, _ := vv["type"].(string); t != "" && t != "bind" {
					continue
				}
				src, _ = vv["source"].(string)
				dst, _ = vv["target"].(string)
			}
			host, ok := resolveInAppData(src, appData)
			if !ok || !strings.HasPrefix(dst, "/") || filepath.Ext(host) != "" || filepath.Ext(dst) != "" || seen[host] {
				continue
			}
			seen[host] = true
			out = append(out, seedMount{image: image, containerPath: dst, hostDir: host})
		}
	}
	return out
}

// seedFromImages fills empty data folders with what the image ships there, the way a named volume
// would on first use. Without it a bind mount hides files the app needs (e.g. a default config).
// Problems only warn: the install goes on and the app reports what it is missing.
func (i *Installer) seedFromImages(ctx context.Context, job *Job, seeds []seedMount) {
	for _, s := range seeds {
		if err := i.MkdirAll(s.hostDir); err != nil {
			slog.Warn("não foi possível criar a pasta de dados", "path", s.hostDir, "err", err)
			continue
		}
		if entries, err := os.ReadDir(s.hostDir); err != nil || len(entries) > 0 {
			continue // missing (cannot copy) or already has data (never overwrite)
		}
		job.emit(Event{Type: "log", Message: "Copiando os arquivos padrão de " + s.image + " (" + s.containerPath + ")"})
		if err := i.Compose.SeedDir(ctx, s.image, s.containerPath, s.hostDir); err != nil {
			slog.Warn("não foi possível copiar a pasta da imagem", "image", s.image, "path", s.containerPath, "err", err)
			job.emit(Event{Type: "log", Message: "Aviso: não foi possível copiar os arquivos padrão: " + err.Error()})
		}
	}
}
