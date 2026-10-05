package catalog

import (
	"fmt"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// convertCasaos converts one ZimaOS/CasaOS app (docker-compose.yml with x-casaos) into a template (SPEC-09).
func convertCasaos(dir string, composeRaw []byte, sourceID string) (*Template, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(composeRaw, &doc); err != nil {
		return nil, fmt.Errorf("docker-compose.yml inválido: %w", err)
	}
	raw := string(composeRaw)
	xc := asMap(doc["x-casaos"])
	if xc == nil {
		return nil, fmt.Errorf("sem x-casaos")
	}
	id := slugify(str(doc["name"]))
	if !ValidID(id) {
		id = slugify(dir)
	}
	if !ValidID(id) {
		return nil, fmt.Errorf("id inválido")
	}

	meta := Meta{
		ID:          id,
		Title:       firstNonEmpty(pickLang(xc["title"]), dir),
		Tagline:     pickLang(xc["tagline"]),
		Description: pickLang(xc["description"]),
		Icon:        firstNonEmpty(str(xc["icon"]), str(xc["thumbnail"])),
		Category:    firstNonEmpty(titleCase(str(xc["category"])), "Other"),
		Scheme:      str(xc["scheme"]),
		Path:        str(xc["index"]),
	}
	if meta.Path == "/" {
		meta.Path = ""
	}

	if archs, ok := xc["architectures"].([]any); ok && len(archs) > 0 {
		match := false
		for _, a := range archs {
			match = match || fmt.Sprint(a) == runtime.GOARCH
		}
		if !match {
			return unsupportedTemplate(meta, raw, fmt.Sprintf("não suporta a arquitetura %s (suporta %s)", runtime.GOARCH, joinAny(archs)), sourceID), nil
		}
	}

	services := asMap(doc["services"])
	if len(services) == 0 {
		return unsupportedTemplate(meta, raw, "compose sem serviços", sourceID), nil
	}
	main := str(xc["main"])
	if _, ok := services[main]; !ok {
		main = sortedKeys(services)[0]
	}
	meta.Main = main

	// /DATA/AppData/<app>/x → ${APPDATA}/x ; any other /DATA/... asks the user for a folder.
	fieldBySource := map[string]string{}
	for _, name := range sortedKeys(services) {
		svc := asMap(services[name])
		delete(svc, "x-casaos")
		rewriteCasaosVolumes(svc, &meta, fieldBySource)
	}
	delete(doc, "x-casaos")
	delete(doc, "name")

	port := toInt(xc["port_map"])
	if mainSvc := asMap(services[main]); mainSvc != nil {
		if port == 0 {
			if ports, _ := mainSvc["ports"].([]any); len(ports) > 0 {
				port, _ = parsePortEntry(ports[0])
			}
		}
		if port > 0 {
			expr := fmt.Sprintf("${PORT:-%d}", port)
			ports, _ := mainSvc["ports"].([]any)
			for i, p := range ports {
				if host, _ := parsePortEntry(p); host == port {
					ports[i] = withHostPort(p, expr)
				}
			}
		}
	}
	meta.Port = port

	known := systemVars()
	for _, f := range meta.Env {
		known[f.Key] = true
	}
	rewrite := func(name string) (string, bool) {
		switch name {
		case "AppID":
			return id, true
		case "WEBUI_PORT":
			if port > 0 {
				return fmt.Sprintf("${PORT:-%d}", port), true
			}
		}
		return "", false
	}
	return finish(meta, doc, rewrite, known, nil, raw, "ZimaOS", sourceID), nil
}

const casaosData = "/DATA/"

func rewriteCasaosVolumes(svc map[string]any, meta *Meta, fieldBySource map[string]string) {
	vols, _ := svc["volumes"].([]any)
	for i, v := range vols {
		switch vv := v.(type) {
		case string:
			src, rest, ok := strings.Cut(vv, ":")
			if !ok {
				continue
			}
			vols[i] = mapCasaosSource(src, meta, fieldBySource) + ":" + rest
		case map[string]any:
			if src, ok := vv["source"].(string); ok {
				vv["source"] = mapCasaosSource(src, meta, fieldBySource)
			}
		}
	}
}

func mapCasaosSource(src string, meta *Meta, fieldBySource map[string]string) string {
	if !strings.HasPrefix(src, casaosData) {
		return src
	}
	if rest, ok := strings.CutPrefix(src, casaosData+"AppData/"); ok {
		_, sub, _ := strings.Cut(rest, "/") // drop the app folder name
		if sub == "" {
			return "${APPDATA}"
		}
		return "${APPDATA}/" + sub
	}
	key, ok := fieldBySource[src]
	if !ok {
		key = fmt.Sprintf("DATA_%d", len(fieldBySource)+1)
		fieldBySource[src] = key
		meta.Env = append(meta.Env, EnvField{Key: key, Label: fmt.Sprintf("Pasta do servidor para %s", src), Type: "path", Required: true})
	}
	return "${" + key + "}"
}
