package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// convertUmbrel converts one Umbrel app (umbrel-app.yml + docker-compose.yml) into a template (SPEC-09).
func convertUmbrel(dir string, manifestRaw, composeRaw []byte, sourceID string) (*Template, error) {
	var mf struct {
		ID              string `yaml:"id"`
		Name            string `yaml:"name"`
		Tagline         string `yaml:"tagline"`
		Icon            string `yaml:"icon"`
		Category        string `yaml:"category"`
		Port            any    `yaml:"port"`
		Description     string `yaml:"description"`
		Path            string `yaml:"path"`
		DefaultUsername string `yaml:"defaultUsername"`
		DefaultPassword string `yaml:"defaultPassword"`
		Dependencies    []any  `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(manifestRaw, &mf); err != nil {
		return nil, fmt.Errorf("umbrel-app.yml inválido: %w", err)
	}
	id := strings.ToLower(strings.TrimSpace(mf.ID))
	if !ValidID(id) {
		id = slugify(dir)
	}
	if len(id) > 40 {
		id = strings.Trim(id[:40], "-")
	}
	if !ValidID(id) {
		return nil, fmt.Errorf("id inválido")
	}

	desc := strings.TrimSpace(mf.Description)
	if u, p := strings.TrimSpace(mf.DefaultUsername), strings.TrimSpace(mf.DefaultPassword); (u != "" && u != "none") || (p != "" && p != "none") {
		desc += fmt.Sprintf("\n\nAcesso inicial — usuário: %s · senha: %s", orDash(u), orDash(p))
	}
	meta := Meta{
		ID: id, Title: firstNonEmpty(mf.Name, id), Tagline: mf.Tagline, Description: desc, Icon: mf.Icon,
		Category: firstNonEmpty(titleCase(mf.Category), "Other"), Path: strings.TrimSpace(mf.Path),
	}
	raw := string(composeRaw)

	if len(mf.Dependencies) > 0 {
		return unsupportedTemplate(meta, raw, fmt.Sprintf("depende de outros apps do Umbrel (%s)", joinAny(mf.Dependencies)), sourceID), nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal(composeRaw, &doc); err != nil {
		return unsupportedTemplate(meta, raw, "docker-compose.yml inválido: "+err.Error(), sourceID), nil
	}
	services := asMap(doc["services"])
	if len(services) == 0 {
		return unsupportedTemplate(meta, raw, "compose sem serviços", sourceID), nil
	}

	// The Umbrel reverse proxy tells which service/port is the web UI. Apps without one publish
	// their own ports, so the published port that matches the manifest is the entry point.
	proxy := asMap(services["app_proxy"])
	host, portRaw := envValue(proxy["environment"], "APP_HOST"), envValue(proxy["environment"], "APP_PORT")
	delete(services, "app_proxy")
	for _, s := range services {
		dropDependsOn(asMap(s), "app_proxy")
	}
	hostPort := toInt(mf.Port)
	var main string
	if proxy != nil {
		main = umbrelMain(services, id, host)
		containerPort := toInt(portRaw)
		if containerPort == 0 && !strings.Contains(portRaw, "$") {
			containerPort = toInt(mf.Port)
		}
		if hostPort == 0 {
			hostPort = containerPort
		}
		switch {
		case main == "":
		case containerPort > 0:
			meta.Port = hostPort
			publishPort(asMap(services[main]), containerPort, fmt.Sprintf("${PORT:-%d}", hostPort))
		case strings.Contains(portRaw, "$"):
			return unsupportedTemplate(meta, raw, "porta do app definida por variável do Umbrel", sourceID), nil
		}
	} else {
		main, meta.Port = umbrelDirectMain(services, hostPort)
		if meta.Port > 0 {
			expr := fmt.Sprintf("${PORT:-%d}", meta.Port)
			ports, _ := asMap(services[main])["ports"].([]any)
			for i, p := range ports {
				if h, _ := parsePortEntry(p); h == meta.Port {
					ports[i] = withHostPort(p, expr)
				}
			}
		}
	}
	if main == "" {
		return unsupportedTemplate(meta, raw, "não foi possível identificar o serviço principal", sourceID), nil
	}
	meta.Main = main

	// Hostnames of the form <app>_<service>_1 become plain service names inside the compose network.
	hostRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `_([A-Za-z0-9_-]+?)_1\b`)
	text, err := encodeYAML(doc)
	if err != nil {
		return unsupportedTemplate(meta, raw, err.Error(), sourceID), nil
	}
	_ = text

	known := systemVars()
	var generate []string
	usesVar := func(name string) bool { return strings.Contains(text, "$"+name) || strings.Contains(text, "${"+name) }
	if usesVar("APP_SEED") {
		generate = append(generate, "APP_SEED")
		known["APP_SEED"] = true
	}
	for _, k := range []string{"APP_DOMAIN", "DEVICE_DOMAIN_NAME"} {
		if usesVar(k) {
			meta.Env = append(meta.Env, EnvField{Key: k, Label: "Endereço do servidor (IP ou nome)", Type: "string", Default: "localhost"})
			known[k] = true
		}
	}
	for _, f := range meta.Env {
		known[f.Key] = true
	}

	rewrite := func(name string) (string, bool) {
		switch name {
		case "APP_DATA_DIR":
			return "${APPDATA}", true
		case "APP_ID":
			return id, true
		}
		return "", false
	}
	// Hostnames are rewritten in the encoded document, then variables are resolved.
	t := finishWithText(meta, doc, hostRe, rewrite, known, generate, raw, "Umbrel", sourceID)
	return t, nil
}

// finishWithText applies the hostname rewrite to the encoded compose and then runs finish.
func finishWithText(meta Meta, doc map[string]any, hostRe *regexp.Regexp, rewrite func(string) (string, bool), known map[string]bool, generate []string, raw, product, sourceID string) *Template {
	text, err := encodeYAML(doc)
	if err != nil {
		return unsupportedTemplate(meta, raw, err.Error(), sourceID)
	}
	text = hostRe.ReplaceAllString(text, "$1")
	var doc2 map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc2); err != nil {
		return unsupportedTemplate(meta, raw, err.Error(), sourceID)
	}
	return finish(meta, doc2, rewrite, known, generate, raw, product, sourceID)
}

// umbrelMain picks the service behind app_proxy.
func umbrelMain(services map[string]any, appID, host string) string {
	host = strings.TrimSpace(host)
	candidates := []string{host, strings.TrimSuffix(host, "_1")}
	trimmed := strings.TrimSuffix(host, "_1")
	candidates = append(candidates, strings.TrimPrefix(trimmed, appID+"_"))
	for _, c := range candidates {
		if _, ok := services[c]; ok && c != "" {
			return c
		}
	}
	if len(services) == 1 {
		for n := range services {
			return n
		}
	}
	return ""
}

// envValue reads KEY from a compose "environment" (map or list form).
func envValue(env any, key string) string {
	switch e := env.(type) {
	case map[string]any:
		return strings.TrimSpace(fmt.Sprint(e[key]))
	case []any:
		for _, item := range e {
			if k, v, ok := strings.Cut(fmt.Sprint(item), "="); ok && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func dropDependsOn(svc map[string]any, name string) {
	switch d := svc["depends_on"].(type) {
	case []any:
		kept := d[:0:0]
		for _, x := range d {
			if fmt.Sprint(x) != name {
				kept = append(kept, x)
			}
		}
		if len(kept) == 0 {
			delete(svc, "depends_on")
		} else {
			svc["depends_on"] = kept
		}
	case map[string]any:
		delete(d, name)
		if len(d) == 0 {
			delete(svc, "depends_on")
		}
	}
}

func orDash(s string) string {
	if s == "" || s == "none" {
		return "—"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// umbrelDirectMain picks the entry service of an app that has no app_proxy: the one publishing the
// manifest port, else the first one publishing any port, else the only service.
func umbrelDirectMain(services map[string]any, manifestPort int) (string, int) {
	names := sortedKeys(services)
	first, firstPort := "", 0
	for _, n := range names {
		ports, _ := asMap(services[n])["ports"].([]any)
		for _, p := range ports {
			h, _ := parsePortEntry(p)
			if h == 0 {
				continue
			}
			if manifestPort > 0 && h == manifestPort {
				return n, h
			}
			if first == "" {
				first, firstPort = n, h
			}
		}
	}
	if first != "" {
		return first, firstPort
	}
	if len(names) == 1 {
		return names[0], 0
	}
	return "", 0
}
