package apps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"omvhome/internal/catalog"
	"omvhome/internal/compose"
	"omvhome/internal/docker"
)

// MaxComposeSize bounds an edited compose or .env file.
const MaxComposeSize = 256 << 10

// ErrInvalidCompose is bad user input when editing a compose file (HTTP 400).
type ErrInvalidCompose struct{ Msg string }

func (e *ErrInvalidCompose) Error() string { return e.Msg }

// ComposeFiles are the editable project files of a managed app.
type ComposeFiles struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

func (s *Service) managedProject(ctx context.Context, id string) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !a.Managed || !s.hasProjectFiles(id) {
		return ErrNotManaged
	}
	return nil
}

// ReadCompose returns the compose and .env files of a managed app.
func (s *Service) ReadCompose(ctx context.Context, id string) (*ComposeFiles, error) {
	if err := s.managedProject(ctx, id); err != nil {
		return nil, err
	}
	dir := s.ProjectDir(id)
	yml, err := os.ReadFile(filepath.Join(dir, compose.ComposeFile))
	if err != nil {
		return nil, err
	}
	env, err := os.ReadFile(filepath.Join(dir, compose.EnvFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &ComposeFiles{Compose: string(yml), Env: string(env)}, nil
}

// validateCompose checks the YAML, that it still carries the app's label (otherwise the app
// would vanish from the list) and, unless allowed, that it adds no unsafe settings.
func validateCompose(id, raw string, allowUnsafe bool) error {
	if len(raw) > MaxComposeSize {
		return &ErrInvalidCompose{"compose grande demais"}
	}
	var doc struct {
		Services map[string]struct {
			Labels any `yaml:"labels"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return &ErrInvalidCompose{"YAML inválido: " + err.Error()}
	}
	if len(doc.Services) == 0 {
		return &ErrInvalidCompose{"o compose precisa ter ao menos um serviço em `services`"}
	}
	labelled := false
	for _, svc := range doc.Services {
		switch l := svc.Labels.(type) {
		case map[string]any:
			labelled = labelled || fmt.Sprint(l[docker.LabelApp]) == id
		case []any:
			for _, item := range l {
				labelled = labelled || fmt.Sprint(item) == docker.LabelApp+"="+id
			}
		}
	}
	if !labelled {
		return &ErrInvalidCompose{fmt.Sprintf("mantenha o label %s=%s em um serviço, senão o app some da lista", docker.LabelApp, id)}
	}
	if !allowUnsafe {
		reasons, err := catalog.UnsafeReasons(raw)
		if err != nil {
			return &ErrInvalidCompose{err.Error()}
		}
		if len(reasons) > 0 {
			return &ErrInvalidCompose{"configurações inseguras exigem confirmação: " + strings.Join(reasons, "; ")}
		}
	}
	return nil
}

// Redeploy replaces the compose and .env files of a managed app and recreates its containers.
// When compose up fails the previous files are restored and brought back up.
func (s *Service) Redeploy(ctx context.Context, id string, files ComposeFiles, allowUnsafe bool, onLine func(string)) error {
	if err := s.managedProject(ctx, id); err != nil {
		return err
	}
	if err := validateCompose(id, files.Compose, allowUnsafe); err != nil {
		return err
	}
	if len(files.Env) > MaxComposeSize {
		return &ErrInvalidCompose{".env grande demais"}
	}
	old, err := s.ReadCompose(ctx, id)
	if err != nil {
		return err
	}
	dir, project := s.ProjectDir(id), compose.ProjectName(id)
	write := func(f ComposeFiles) error {
		if err := os.WriteFile(filepath.Join(dir, compose.ComposeFile), []byte(f.Compose), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, compose.EnvFile), []byte(f.Env), 0o600)
	}
	if bytes.Equal([]byte(old.Compose), []byte(files.Compose)) && old.Env == files.Env {
		return s.Compose.Up(ctx, project, dir, onLine)
	}
	if err := write(files); err != nil {
		_ = write(*old)
		return err
	}
	if err := s.Compose.Up(ctx, project, dir, onLine); err != nil {
		_ = write(*old)
		_ = s.Compose.Up(context.WithoutCancel(ctx), project, dir, nil)
		return err
	}
	return nil
}
