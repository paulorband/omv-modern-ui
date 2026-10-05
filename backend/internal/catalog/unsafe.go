package catalog

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnsafeReasons lists risky settings in a compose document (SPEC-05 R05.7, SPEC-09).
func UnsafeReasons(raw string) ([]string, error) {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("YAML inválido: %w", err)
	}
	if len(doc.Services) == 0 {
		return nil, fmt.Errorf("o compose precisa ter ao menos um serviço em `services`")
	}
	var out []string
	for _, n := range sortedKeys(doc.Services) {
		svc := doc.Services[n]
		if p, _ := svc["privileged"].(bool); p {
			out = append(out, n+": privileged: true")
		}
		if nm, _ := svc["network_mode"].(string); nm == "host" {
			out = append(out, n+": network_mode: host")
		}
		if pid, _ := svc["pid"].(string); pid == "host" {
			out = append(out, n+": pid: host")
		}
		if vols, ok := svc["volumes"].([]any); ok {
			for _, v := range vols {
				src := ""
				switch vv := v.(type) {
				case string:
					src, _, _ = strings.Cut(vv, ":")
				case map[string]any:
					src, _ = vv["source"].(string)
				}
				if strings.Contains(src, "docker.sock") || src == "/" {
					out = append(out, n+": volume "+src)
				}
			}
		}
	}
	return out, nil
}

// Warnings lists settings that deserve a look but do not block installation (SPEC-09).
func Warnings(raw string) []string {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(raw), &doc) != nil {
		return nil
	}
	var out []string
	for _, n := range sortedKeys(doc.Services) {
		svc := doc.Services[n]
		if caps, ok := svc["cap_add"].([]any); ok && len(caps) > 0 {
			out = append(out, fmt.Sprintf("%s: capacidades extras (%s)", n, joinAny(caps)))
		}
		if devs, ok := svc["devices"].([]any); ok && len(devs) > 0 {
			out = append(out, fmt.Sprintf("%s: acesso a dispositivos (%s)", n, joinAny(devs)))
		}
		if so, ok := svc["security_opt"].([]any); ok && len(so) > 0 {
			out = append(out, fmt.Sprintf("%s: security_opt (%s)", n, joinAny(so)))
		}
		if ipc, _ := svc["ipc"].(string); ipc == "host" {
			out = append(out, n+": ipc: host")
		}
	}
	return out
}

func joinAny(v []any) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return strings.Join(parts, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
