package docker

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ValidationError marks bad user input (mapped to HTTP 400).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

var (
	envKeyRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	restartOK = map[string]bool{"": true, "no": true, "always": true, "unless-stopped": true, "on-failure": true}
)

// Validate checks the spec before it reaches Docker.
func (s RunSpec) Validate() error {
	if strings.TrimSpace(s.Image) == "" || strings.ContainsAny(s.Image, " \t\n") {
		return invalid("imagem inválida")
	}
	if s.Name != "" && !nameRe.MatchString(s.Name) {
		return invalid("nome de container inválido")
	}
	if !restartOK[s.Restart] {
		return invalid("política de restart inválida: %q", s.Restart)
	}
	for _, p := range s.Ports {
		if p.Container < 1 || p.Container > 65535 || p.Host < 0 || p.Host > 65535 {
			return invalid("porta inválida (%d:%d)", p.Host, p.Container)
		}
		if p.Protocol != "" && p.Protocol != "tcp" && p.Protocol != "udp" {
			return invalid("protocolo inválido: %q", p.Protocol)
		}
	}
	for _, v := range s.Volumes {
		if v.Host == "" || !path.IsAbs(v.Container) {
			return invalid("volume inválido (%q:%q)", v.Host, v.Container)
		}
		if strings.Contains(v.Host, ":") && !strings.HasPrefix(v.Host, "/") {
			return invalid("origem de volume inválida: %q", v.Host)
		}
	}
	for k := range s.Env {
		if !envKeyRe.MatchString(k) {
			return invalid("nome de variável inválido: %q", k)
		}
	}
	return nil
}

// createBody renders the /containers/create payload.
func (s RunSpec) createBody() map[string]any {
	host := map[string]any{}
	body := map[string]any{"Image": s.Image, "HostConfig": host}

	if len(s.Env) > 0 {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys))
		for _, k := range keys {
			env = append(env, k+"="+s.Env[k])
		}
		body["Env"] = env
	}
	if len(s.Command) > 0 {
		body["Cmd"] = s.Command
	}
	if s.User != "" {
		body["User"] = s.User
	}
	if s.Hostname != "" {
		body["Hostname"] = s.Hostname
	}
	if len(s.Labels) > 0 {
		body["Labels"] = s.Labels
	}
	if len(s.Ports) > 0 {
		exposed := map[string]struct{}{}
		bindings := map[string][]map[string]string{}
		for _, p := range s.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			key := strconv.Itoa(p.Container) + "/" + proto
			exposed[key] = struct{}{}
			b := map[string]string{"HostPort": ""}
			if p.Host > 0 {
				b["HostPort"] = strconv.Itoa(p.Host)
			}
			if p.HostIP != "" {
				b["HostIp"] = p.HostIP
			}
			bindings[key] = append(bindings[key], b)
		}
		body["ExposedPorts"] = exposed
		host["PortBindings"] = bindings
	}
	if len(s.Volumes) > 0 {
		binds := make([]string, 0, len(s.Volumes))
		for _, v := range s.Volumes {
			b := v.Host + ":" + v.Container
			if v.ReadOnly {
				b += ":ro"
			}
			binds = append(binds, b)
		}
		host["Binds"] = binds
	}
	if s.Restart != "" {
		host["RestartPolicy"] = map[string]string{"Name": s.Restart}
	}
	if s.Network != "" {
		host["NetworkMode"] = s.Network
	}
	if s.Privileged {
		host["Privileged"] = true
	}
	return body
}
