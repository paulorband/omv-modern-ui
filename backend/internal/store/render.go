package store

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"omvhome/internal/catalog"

	"gopkg.in/yaml.v3"
)

// dotenvQuote formats a value for a compose .env file so it is taken literally. Single quotes
// disable interpolation; if the value itself has a single quote, double quotes with escapes are used.
func dotenvQuote(v string) string {
	if !strings.ContainsAny(v, " \t#$\"'\\`") && v != "" {
		return v
	}
	if !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(v) + `"`
}

type envKV struct{ Key, Value string }

func renderDotenv(vars []envKV) []byte {
	var b bytes.Buffer
	for _, kv := range vars {
		b.WriteString(kv.Key + "=" + dotenvQuote(kv.Value) + "\n")
	}
	return b.Bytes()
}

// injectLabels adds labels to the main service and drops the x-omvhome extension.
func injectLabels(raw string, main string, labels map[string]string) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("YAML inválido: %w", err)
	}
	services, _ := doc["services"].(map[string]any)
	svc, _ := services[main].(map[string]any)
	if svc == nil {
		return nil, fmt.Errorf("serviço principal %q não encontrado", main)
	}
	merged := map[string]any{}
	switch l := svc["labels"].(type) {
	case map[string]any:
		for k, v := range l {
			merged[k] = fmt.Sprint(v)
		}
	case []any:
		for _, item := range l {
			if k, v, ok := strings.Cut(fmt.Sprint(item), "="); ok {
				merged[k] = v
			}
		}
	}
	for k, v := range labels {
		merged[k] = v
	}
	svc["labels"] = merged
	delete(doc, "x-omvhome")

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a display name to an app id.
func slugify(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// serviceNames returns the service names of a compose document, sorted.
func serviceNames(raw string) ([]string, error) {
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("YAML inválido: %w", err)
	}
	if len(doc.Services) == 0 {
		return nil, fmt.Errorf("o compose precisa ter ao menos um serviço em `services`")
	}
	names := make([]string, 0, len(doc.Services))
	for n := range doc.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// unsafeReasons lists risky settings in a compose document (R05.7).
func unsafeReasons(raw string) ([]string, error) { return catalog.UnsafeReasons(raw) }
