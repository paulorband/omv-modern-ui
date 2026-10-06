package catalog

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Shared helpers for converting third-party app formats into templates (SPEC-09).

var (
	// $$ (escape) | ${NAME<rest>} | $NAME
	varRe  = regexp.MustCompile(`\$(\$|\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)
	slugRe = regexp.MustCompile(`[^a-z0-9]+`)
)

func slugify(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// mapVars rewrites the variable references fn accepts; "$$" escapes are left alone.
func mapVars(text string, fn func(name string) (string, bool)) string {
	return varRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := varRe.FindStringSubmatch(m)
		if sub[1] == "$" {
			return m
		}
		name := sub[2]
		if name == "" {
			name = sub[4]
		}
		if r, ok := fn(name); ok {
			return r
		}
		return m
	})
}

// unknownVars lists variables that are neither known nor given a default in the text.
func unknownVars(text string, known map[string]bool) []string {
	seen := map[string]bool{}
	for _, sub := range varRe.FindAllStringSubmatch(text, -1) {
		if sub[1] == "$" {
			continue
		}
		name, rest := sub[2], sub[3]
		if name == "" {
			name = sub[4]
		}
		if known[name] || strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, ":-") || strings.HasPrefix(rest, "+") || strings.HasPrefix(rest, ":+") {
			continue
		}
		seen[name] = true
	}
	return sortedKeys(seen)
}

func systemVars() map[string]bool {
	known := map[string]bool{}
	for k := range ReservedVars {
		known[k] = true
	}
	return known
}

// pickLang chooses a localized string: pt_BR, then English, then the first key.
func pickLang(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		for _, k := range []string{"pt_BR", "pt_PT", "en_US", "en_GB", "en"} {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		for _, k := range sortedKeys(t) {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case uint64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return n
		}
	}
	return 0
}

func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func encodeYAML(v any) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	_ = enc.Close()
	return buf.String(), nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// parsePortEntry returns the host and container ports of a compose "ports" entry (0 when absent).
func parsePortEntry(e any) (host, container int) {
	switch v := e.(type) {
	case int:
		return 0, v
	case string:
		s := v
		if i := strings.Index(s, "/"); i >= 0 {
			s = s[:i]
		}
		parts := strings.Split(s, ":")
		container = toInt(parts[len(parts)-1])
		if len(parts) >= 2 {
			host = toInt(parts[len(parts)-2])
		}
	case map[string]any:
		return toInt(v["published"]), toInt(v["target"])
	}
	return host, container
}

// withHostPort rewrites the published (host) port of a "ports" entry to expr.
func withHostPort(e any, expr string) any {
	switch v := e.(type) {
	case string:
		s, proto := v, ""
		if i := strings.Index(s, "/"); i >= 0 {
			s, proto = s[:i], s[i:]
		}
		parts := strings.Split(s, ":")
		if len(parts) >= 2 {
			parts[len(parts)-2] = expr
		}
		return strings.Join(parts, ":") + proto
	case map[string]any:
		nv := make(map[string]any, len(v))
		for k, x := range v {
			nv[k] = x
		}
		nv["published"] = expr
		return nv
	}
	return e
}

// publishPort makes the service publish containerPort on the host through hostExpr. Existing entries
// for that container port get their host side rewritten; otherwise "hostExpr:containerExpr" is added,
// which is reported by the return value.
func publishPort(svc map[string]any, containerPort int, hostExpr, containerExpr string) (added bool) {
	ports, _ := svc["ports"].([]any)
	found := false
	for i, p := range ports {
		if host, cont := parsePortEntry(p); cont == containerPort && host != 0 {
			ports[i] = withHostPort(p, hostExpr)
			found = true
		}
	}
	if !found {
		ports = append(ports, hostExpr+":"+containerExpr)
		added = true
	}
	svc["ports"] = ports
	return added
}

// finish turns a converted compose document into a template: it rewrites variables, rejects
// leftovers, validates through Parse and classifies risk. product names the origin in messages.
func finish(meta Meta, doc map[string]any, rewrite func(name string) (string, bool), known map[string]bool, generate []string, rawCompose, product, sourceID string) *Template {
	body, err := encodeYAML(doc)
	if err != nil {
		return unsupportedTemplate(meta, rawCompose, "compose inválido: "+err.Error(), sourceID)
	}
	body = mapVars(body, rewrite)
	if unknown := unknownVars(body, known); len(unknown) > 0 {
		return unsupportedTemplate(meta, rawCompose, fmt.Sprintf("usa variáveis do %s sem equivalente aqui: %s", product, strings.Join(unknown, ", ")), sourceID)
	}
	metaYAML, err := encodeYAML(map[string]any{"x-omvhome": meta})
	if err != nil {
		return unsupportedTemplate(meta, rawCompose, err.Error(), sourceID)
	}
	full := body + metaYAML
	t, err := Parse([]byte(full))
	if err != nil {
		return unsupportedTemplate(meta, rawCompose, err.Error(), sourceID)
	}
	t.Source, t.Generate = sourceID, generate
	if unsafe, err := UnsafeReasons(body); err == nil {
		t.Unsafe = unsafe
	}
	t.Warnings = Warnings(body)
	return t
}

// unsupportedTemplate keeps an app visible in the catalog with the reason it cannot be installed.
func unsupportedTemplate(meta Meta, rawCompose, reason, sourceID string) *Template {
	if meta.Title == "" {
		meta.Title = meta.ID
	}
	if meta.Category == "" {
		meta.Category = "Other"
	}
	return &Template{Meta: meta, Compose: rawCompose, Source: sourceID, Unsupported: reason}
}
