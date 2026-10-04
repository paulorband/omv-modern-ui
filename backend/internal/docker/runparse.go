package docker

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRunCommand converts a `docker run ...` command line into a RunSpec (R03.8).
// Supported: -d --name -p -v -e --restart --network/--net -u/--user -h/--hostname --privileged.
// Any other flag makes the call fail, listing the unsupported flags.
func ParseRunCommand(cmdline string) (RunSpec, error) {
	cmdline = strings.ReplaceAll(cmdline, "\\\r\n", " ")
	cmdline = strings.ReplaceAll(cmdline, "\\\n", " ")
	toks, err := shellSplit(cmdline)
	if err != nil {
		return RunSpec{}, err
	}
	if len(toks) > 0 && toks[0] == "docker" {
		toks = toks[1:]
	}
	if len(toks) == 0 || toks[0] != "run" {
		return RunSpec{}, invalid("o comando deve começar com `docker run`")
	}
	toks = toks[1:]

	spec := RunSpec{Env: map[string]string{}}
	var unknown []string
	i := 0
	// value returns the flag value for "--flag value" or "--flag=value".
	value := func(flag, inline string, hasInline bool) (string, error) {
		if hasInline {
			return inline, nil
		}
		i++
		if i >= len(toks) {
			return "", invalid("flag %s exige um valor", flag)
		}
		return toks[i], nil
	}
	for ; i < len(toks); i++ {
		t := toks[i]
		if !strings.HasPrefix(t, "-") || t == "-" {
			break // first positional = image
		}
		flag, inline, hasInline := t, "", false
		if strings.HasPrefix(t, "--") {
			flag, inline, hasInline = strings.Cut(t, "=")
		}
		switch flag {
		case "-d", "--detach":
		case "--privileged":
			spec.Privileged = true
		case "--name", "-p", "--publish", "-v", "--volume", "-e", "--env", "--restart",
			"--network", "--net", "-u", "--user", "-h", "--hostname":
			v, err := value(flag, inline, hasInline)
			if err != nil {
				return RunSpec{}, err
			}
			if err := applyRunFlag(&spec, flag, v); err != nil {
				return RunSpec{}, err
			}
		default:
			unknown = append(unknown, flag)
		}
	}
	if len(unknown) > 0 {
		return RunSpec{}, invalid("flags não suportadas: %s", strings.Join(unknown, ", "))
	}
	if i >= len(toks) {
		return RunSpec{}, invalid("imagem não informada")
	}
	spec.Image = toks[i]
	spec.Command = toks[i+1:]
	if len(spec.Env) == 0 {
		spec.Env = nil
	}
	return spec, spec.Validate()
}

func applyRunFlag(s *RunSpec, flag, v string) error {
	switch flag {
	case "--name":
		s.Name = v
	case "-p", "--publish":
		p, err := parsePortSpec(v)
		if err != nil {
			return err
		}
		s.Ports = append(s.Ports, p)
	case "-v", "--volume":
		vol, err := parseVolumeSpec(v)
		if err != nil {
			return err
		}
		s.Volumes = append(s.Volumes, vol)
	case "-e", "--env":
		k, val, _ := strings.Cut(v, "=")
		s.Env[k] = val
	case "--restart":
		s.Restart = v
	case "--network", "--net":
		s.Network = v
	case "-u", "--user":
		s.User = v
	case "-h", "--hostname":
		s.Hostname = v
	}
	return nil
}

// parsePortSpec parses "[ip:][host:]container[/proto]".
func parsePortSpec(v string) (RunPort, error) {
	var p RunPort
	body, proto, hasProto := strings.Cut(v, "/")
	if hasProto {
		p.Protocol = proto
	}
	parts := strings.Split(body, ":")
	atoi := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, invalid("porta inválida: %q", v)
		}
		return n, nil
	}
	var err error
	switch len(parts) {
	case 1:
		p.Container, err = atoi(parts[0])
	case 2:
		if p.Host, err = atoi(parts[0]); err == nil {
			p.Container, err = atoi(parts[1])
		}
	case 3:
		p.HostIP = parts[0]
		if p.Host, err = atoi(parts[1]); err == nil {
			p.Container, err = atoi(parts[2])
		}
	default:
		err = invalid("porta inválida: %q", v)
	}
	return p, err
}

// parseVolumeSpec parses "host:container[:ro]" (host may be a named volume).
func parseVolumeSpec(v string) (RunVolume, error) {
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 2:
		return RunVolume{Host: parts[0], Container: parts[1]}, nil
	case 3:
		return RunVolume{Host: parts[0], Container: parts[1], ReadOnly: strings.Contains(parts[2], "ro")}, nil
	}
	return RunVolume{}, invalid("volume inválido: %q", v)
}

// shellSplit splits on whitespace honouring single and double quotes and backslash escapes
// (outside single quotes). It performs no expansion of any kind.
func shellSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inTok := false
	var quote rune
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\'):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inTok = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			inTok = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inTok {
				out = append(out, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("%w", invalid("aspas não fechadas"))
	}
	if inTok {
		out = append(out, cur.String())
	}
	return out, nil
}
