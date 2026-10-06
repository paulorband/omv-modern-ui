// Package compose runs `docker compose` for app projects. Arguments are always passed as a
// list (never through a shell) so user-controlled values cannot inject commands (A05.7).
package compose

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ProjectName is the compose project name used for an app id.
func ProjectName(appID string) string { return "omvhome-" + appID }

// ComposeFile / EnvFile are the file names inside an app's project directory.
const (
	ComposeFile = "docker-compose.yml"
	EnvFile     = ".env"
)

// Runner executes compose operations on a project directory.
type Runner interface {
	Available(ctx context.Context) bool
	Up(ctx context.Context, project, dir string, onLine func(string)) error
	Start(ctx context.Context, project, dir string) error
	Stop(ctx context.Context, project, dir string) error
	Restart(ctx context.Context, project, dir string) error
	Pull(ctx context.Context, project, dir string, onLine func(string)) error
	Down(ctx context.Context, project, dir string) error
	// SeedDir copies what image has at containerPath into hostDir, as a named volume would on first
	// use. A path the image does not have is not an error (SPEC-09 R09.11).
	SeedDir(ctx context.Context, image, containerPath, hostDir string) error
	// ImageUser reports the numeric uid:gid an image runs as (its USER); ok is false for root or when unknown.
	ImageUser(ctx context.Context, image string) (uid, gid int, ok bool)
}

// Executor runs `docker <args...>` in dir, reporting each output line. Tests replace it.
type Executor func(ctx context.Context, dir string, args []string, onLine func(string)) error

// ExecRunner implements Runner by invoking the docker CLI.
type ExecRunner struct {
	Exec Executor
}

func NewExecRunner() *ExecRunner { return &ExecRunner{Exec: osExec} }

func osExec(ctx context.Context, dir string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker compose: %w", err)
	}
	var tail []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if onLine != nil {
				onLine(line)
			}
			if tail = append(tail, line); len(tail) > 5 {
				tail = tail[1:]
			}
		}
	}()
	err := cmd.Wait()
	_ = pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("docker %s falhou: %w: %s", strings.Join(args[:min(len(args), 4)], " "), err, strings.Join(tail, " | "))
	}
	return nil
}

func (r *ExecRunner) args(project, dir string, verb ...string) []string {
	a := []string{"compose", "-p", project, "--project-directory", dir, "-f", filepath.Join(dir, ComposeFile)}
	return append(a, verb...)
}

func (r *ExecRunner) Available(ctx context.Context) bool {
	return r.Exec(ctx, "", []string{"compose", "version"}, nil) == nil
}

func (r *ExecRunner) Up(ctx context.Context, project, dir string, onLine func(string)) error {
	return r.Exec(ctx, dir, r.args(project, dir, "up", "-d", "--remove-orphans"), onLine)
}
func (r *ExecRunner) Start(ctx context.Context, project, dir string) error {
	return r.Exec(ctx, dir, r.args(project, dir, "start"), nil)
}
func (r *ExecRunner) Stop(ctx context.Context, project, dir string) error {
	return r.Exec(ctx, dir, r.args(project, dir, "stop"), nil)
}
func (r *ExecRunner) Restart(ctx context.Context, project, dir string) error {
	return r.Exec(ctx, dir, r.args(project, dir, "restart"), nil)
}
func (r *ExecRunner) Pull(ctx context.Context, project, dir string, onLine func(string)) error {
	return r.Exec(ctx, dir, r.args(project, dir, "pull"), onLine)
}
func (r *ExecRunner) Down(ctx context.Context, project, dir string) error {
	return r.Exec(ctx, dir, r.args(project, dir, "down", "--remove-orphans"), nil)
}

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}`)

// Interpolate resolves ${VAR} and ${VAR:-default} using vars (compose semantics, minimal).
func Interpolate(s string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := varRe.FindStringSubmatch(m)
		if v, ok := vars[sub[1]]; ok && v != "" {
			return v
		}
		return sub[2]
	})
}

// ErrUnavailable is returned when docker compose is not installed.
var ErrUnavailable = errors.New("docker compose indisponível")

var containerIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (r *ExecRunner) SeedDir(ctx context.Context, image, containerPath, hostDir string) error {
	if image == "" || strings.HasPrefix(image, "-") || !strings.HasPrefix(containerPath, "/") || !filepath.IsAbs(hostDir) {
		return fmt.Errorf("argumentos inválidos para copiar a pasta da imagem")
	}
	// `create` pulls the image when it is missing and never starts it, so no entrypoint runs.
	var id string
	if err := r.Exec(ctx, "", []string{"create", "--entrypoint", "true", "--", image}, func(line string) {
		if l := strings.TrimSpace(line); containerIDRe.MatchString(l) {
			id = l
		}
	}); err != nil {
		return err
	}
	if id == "" {
		return errors.New("docker create não retornou o id do container")
	}
	defer func() { _ = r.Exec(context.WithoutCancel(ctx), "", []string{"rm", "-f", id}, nil) }()
	err := r.Exec(ctx, "", []string{"cp", "-a", id + ":" + strings.TrimRight(containerPath, "/") + "/.", hostDir}, nil)
	if err != nil && strings.Contains(err.Error(), "Could not find the file") {
		return nil
	}
	return err
}

var userNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_.-]*$`)

// lastLine runs docker and returns the last non-empty output line.
func (r *ExecRunner) lastLine(ctx context.Context, args ...string) (string, error) {
	var last string
	err := r.Exec(ctx, "", args, func(line string) {
		if l := strings.TrimSpace(line); l != "" {
			last = l
		}
	})
	return last, err
}

func (r *ExecRunner) ImageUser(ctx context.Context, image string) (int, int, bool) {
	if image == "" || strings.HasPrefix(image, "-") {
		return 0, 0, false
	}
	user, err := r.lastLine(ctx, "image", "inspect", "-f", "{{.Config.User}}", "--", image)
	if err != nil { // not pulled yet
		if r.Exec(ctx, "", []string{"pull", "-q", "--", image}, nil) != nil {
			return 0, 0, false
		}
		if user, err = r.lastLine(ctx, "image", "inspect", "-f", "{{.Config.User}}", "--", image); err != nil {
			return 0, 0, false
		}
	}
	name, group, _ := strings.Cut(user, ":")
	uid, uerr := strconv.Atoi(name)
	if uerr != nil { // a name: ask the image itself
		if !userNameRe.MatchString(name) {
			return 0, 0, false
		}
		out, err := r.lastLine(ctx, "run", "--rm", "--entrypoint", "id", "--", image, "-u", name)
		if uid, err = strconv.Atoi(out); err != nil {
			return 0, 0, false
		}
	}
	gid := -1
	switch g, gerr := strconv.Atoi(group); {
	case group != "" && gerr == nil:
		gid = g
	case group == "" && uerr != nil:
		if out, err := r.lastLine(ctx, "run", "--rm", "--entrypoint", "id", "--", image, "-g", name); err == nil {
			if g, err := strconv.Atoi(out); err == nil {
				gid = g
			}
		}
	}
	return uid, gid, uid > 0
}
