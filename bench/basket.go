package bench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxLabelValueLen = 256

const taskMarkerPrefix = "task:"

var (
	idCharset         = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	checkpointCharset = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
)

var (
	ErrEmptyBasketName = errors.New("basket name is empty")
	ErrBasketNameLen   = errors.New("basket name exceeds 256 bytes")
	ErrReps            = errors.New("reps must be >= 1")
	ErrNoTasks         = errors.New("at least one task is required")
	ErrNoVariants      = errors.New("at least one variant is required")
	ErrEmptyID         = errors.New("id is empty")
	ErrIDLen           = errors.New("id exceeds 256 bytes")
	ErrCharset         = errors.New("value has characters outside [A-Za-z0-9._-]")
	ErrDuplicateID     = errors.New("duplicate id")
	ErrEmptyCmd        = errors.New("task cmd is empty")
	ErrRunIDCollision  = errors.New("run-id collision")
	ErrCheckpoint      = errors.New("invalid checkpoint")
	ErrTimeout         = errors.New("invalid timeout")
	ErrVerifyCmd       = errors.New("verify cmd is empty")
	ErrArtifactGlob    = errors.New("invalid artifact glob")
	ErrWorkspaceCmd    = errors.New("workspace cmd is empty")
	ErrWorkspaceDir    = errors.New("dir and workspace are mutually exclusive")
	ErrWorkspacePatch  = errors.New("workspace patch unreadable")
)

type Task struct {
	ID          string            `yaml:"id" json:"id"`
	Cmd         []string          `yaml:"cmd" json:"cmd"`
	Dir         string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Env         map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Checkpoints []string          `yaml:"checkpoints,omitempty" json:"checkpoints,omitempty"`
	Timeout     string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Artifacts   []string          `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Verify      *Verify           `yaml:"verify,omitempty" json:"verify,omitempty"`
	Workspace   *Workspace        `yaml:"workspace,omitempty" json:"workspace,omitempty"`
}

type Workspace struct {
	Cmd         []string `yaml:"cmd" json:"cmd"`
	Patch       string   `yaml:"patch,omitempty" json:"patch,omitempty"`
	Rev         string   `yaml:"rev,omitempty" json:"rev,omitempty"`
	Teardown    []string `yaml:"teardown,omitempty" json:"teardown,omitempty"`
	PatchAbs    string   `yaml:"-" json:"-"`
	PatchSHA256 string   `yaml:"-" json:"-"`
}

func (t Task) TimeoutDuration() (time.Duration, error) {
	return parseTimeout(t.Timeout)
}

type Verify struct {
	Cmd     []string          `yaml:"cmd" json:"cmd"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Timeout string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

func (v Verify) TimeoutDuration() (time.Duration, error) {
	if v.Timeout == "" {
		return time.Minute, nil
	}
	return parseTimeout(v.Timeout)
}

func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf(`%w: %q (use a duration with units, e.g. "30s")`, ErrTimeout, s)
	}
	if d < 0 {
		return 0, fmt.Errorf("%w: %q must not be negative", ErrTimeout, s)
	}
	return d, nil
}

type Variant struct {
	ID        string            `yaml:"id" json:"id"`
	Env       map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Setup     []string          `yaml:"setup,omitempty" json:"setup,omitempty"`
	Workspace *Workspace        `yaml:"workspace,omitempty" json:"workspace,omitempty"`
}

type Basket struct {
	Name     string    `yaml:"basket" json:"basket"`
	Reps     int       `yaml:"reps" json:"reps"`
	Tasks    []Task    `yaml:"tasks" json:"tasks"`
	Variants []Variant `yaml:"variants" json:"variants"`
}

type Cell struct {
	Task    Task
	Variant Variant
	Rep     int
	RunID   string
	Labels  map[string]string
}

func (c Cell) EffectiveWorkspace() *Workspace {
	if c.Variant.Workspace != nil {
		return c.Variant.Workspace
	}
	return c.Task.Workspace
}

func Load(path string) (Basket, string, error) {
	b, hash, err := decodeBasket(path)
	if err != nil {
		return Basket{}, "", err
	}
	if err := resolvePatches(&b, filepath.Dir(path)); err != nil {
		return Basket{}, "", err
	}
	return b, hash, nil
}

func LoadOffline(path string) (Basket, string, error) {
	return decodeBasket(path)
}

func decodeBasket(path string) (Basket, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Basket{}, "", fmt.Errorf("bench.Load: %w", err)
	}
	var b Basket
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&b); err != nil {
		return Basket{}, "", fmt.Errorf("bench.Load: %w", humanizeDecodeErr(err))
	}
	if err := validate(b); err != nil {
		return Basket{}, "", err
	}
	resolveExecPaths(&b, filepath.Dir(path))
	sum := sha256.Sum256(data)
	return b, hex.EncodeToString(sum[:]), nil
}

var yamlKindHuman = map[string]string{
	"!!str": "a single value", "!!int": "a number", "!!seq": "a list",
	"!!map": "a mapping", "!!bool": "a true/false value", "!!float": "a number",
}

var goTypeHuman = map[string]string{
	"[]string": "a list of strings", "int": "a whole number",
	"string": "a single value", "map[string]string": "a mapping of strings",
	"[]bench.Task": "a list of tasks", "[]bench.Variant": "a list of variants",
}

var decodeTypeRe = regexp.MustCompile(`cannot unmarshal (\S+)(?: .+?)? into (\S+)`)

func humanizeDecodeErr(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	lines := make([]string, 0, len(te.Errors))
	for _, m := range te.Errors {
		lines = append(lines, decodeTypeRe.ReplaceAllStringFunc(m, func(s string) string {
			g := decodeTypeRe.FindStringSubmatch(s)
			got, ok1 := yamlKindHuman[g[1]]
			want, ok2 := goTypeHuman[g[2]]
			if !ok1 || !ok2 {
				return s
			}
			return "expected " + want + ", but got " + got
		}))
	}
	return errors.New(strings.Join(lines, "; "))
}

func resolveExecPaths(b *Basket, baseDir string) {
	for i := range b.Tasks {
		t := &b.Tasks[i]
		if t.Dir != "" && !filepath.IsAbs(t.Dir) {
			t.Dir = filepath.Join(baseDir, t.Dir)
		}
		resolveArgvRel(t.Cmd, baseDir)
		if t.Verify != nil {
			resolveArgvRel(t.Verify.Cmd, baseDir)
		}
	}
}

func resolveArgvRel(argv []string, baseDir string) {
	for i, a := range argv {
		if strings.HasPrefix(a, "./") || strings.HasPrefix(a, "../") {
			argv[i] = filepath.Join(baseDir, a)
		}
	}
}

func resolvePatches(b *Basket, baseDir string) error {
	for i := range b.Tasks {
		if err := resolvePatch(b.Tasks[i].Workspace, baseDir); err != nil {
			return fmt.Errorf("bench.Load: task[%d].workspace.patch: %w", i, err)
		}
	}
	for i := range b.Variants {
		if err := resolvePatch(b.Variants[i].Workspace, baseDir); err != nil {
			return fmt.Errorf("bench.Load: variant[%d].workspace.patch: %w", i, err)
		}
	}
	return nil
}

var absFn = filepath.Abs

func resolvePatch(w *Workspace, baseDir string) error {
	if w == nil || w.Patch == "" {
		return nil
	}
	joined := w.Patch
	if !filepath.IsAbs(joined) {
		joined = filepath.Join(baseDir, joined)
	}
	abs, err := absFn(joined)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWorkspacePatch, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWorkspacePatch, err)
	}
	sum := sha256.Sum256(data)
	w.PatchAbs = abs
	w.PatchSHA256 = hex.EncodeToString(sum[:])
	return nil
}

func validate(b Basket) error {
	if b.Name == "" {
		return fmt.Errorf("bench.Load: basket: %w", ErrEmptyBasketName)
	}
	if len(b.Name) > maxLabelValueLen {
		return fmt.Errorf("bench.Load: basket: %w", ErrBasketNameLen)
	}
	if !idCharset.MatchString(b.Name) {
		return fmt.Errorf("bench.Load: basket %q: %w", b.Name, ErrCharset)
	}
	if b.Reps < 1 {
		return fmt.Errorf("bench.Load: reps: %w", ErrReps)
	}
	if len(b.Tasks) == 0 {
		return fmt.Errorf("bench.Load: tasks: %w", ErrNoTasks)
	}
	if len(b.Variants) == 0 {
		return fmt.Errorf("bench.Load: variants: %w", ErrNoVariants)
	}
	if err := validateTasks(b.Tasks); err != nil {
		return err
	}
	if err := validateVariants(b.Variants); err != nil {
		return err
	}
	if err := validateWorkspaceDirExclusion(b); err != nil {
		return err
	}
	return validateRunIDs(b)
}

func validateRunIDs(b Basket) error {
	type pair struct{ task, variant string }
	seen := make(map[string]pair, len(b.Tasks)*len(b.Variants))
	for _, t := range b.Tasks {
		for _, v := range b.Variants {
			id := runID(b.Name, t.ID, v.ID, 1)
			if prev, dup := seen[id]; dup {
				return fmt.Errorf(
					"bench.Load: %w: task %q/variant %q and task %q/variant %q",
					ErrRunIDCollision, prev.task, prev.variant, t.ID, v.ID,
				)
			}
			seen[id] = pair{task: t.ID, variant: v.ID}
		}
	}
	return nil
}

func runID(basket, task, variant string, rep int) string {
	return fmt.Sprintf("bench-%s-%s-%s-r%d", basket, task, variant, rep)
}

func validateTasks(tasks []Task) error {
	seen := make(map[string]struct{}, len(tasks))
	for i, t := range tasks {
		if err := checkID("task", i, t.ID, seen); err != nil {
			return err
		}
		if len(t.Cmd) == 0 {
			return fmt.Errorf("bench.Load: task[%d].cmd: %w", i, ErrEmptyCmd)
		}
		if _, err := t.TimeoutDuration(); err != nil {
			return fmt.Errorf("bench.Load: task[%d].timeout: %w", i, err)
		}
		if err := validateCheckpoints(i, t); err != nil {
			return err
		}
		if err := validateVerify(i, t); err != nil {
			return err
		}
		if err := validateArtifacts(i, t); err != nil {
			return err
		}
		if t.Workspace != nil && len(t.Workspace.Cmd) == 0 {
			return fmt.Errorf("bench.Load: task[%d].workspace.cmd: %w", i, ErrWorkspaceCmd)
		}
		if t.Workspace != nil && t.Dir != "" {
			return fmt.Errorf("bench.Load: task[%d]: %w", i, ErrWorkspaceDir)
		}
	}
	return nil
}

func validateVerify(taskIdx int, t Task) error {
	if t.Verify == nil {
		return nil
	}
	if len(t.Verify.Cmd) == 0 {
		return fmt.Errorf("bench.Load: task[%d].verify.cmd: %w", taskIdx, ErrVerifyCmd)
	}
	if _, err := t.Verify.TimeoutDuration(); err != nil {
		return fmt.Errorf("bench.Load: task[%d].verify.timeout: %w", taskIdx, err)
	}
	return nil
}

func validateArtifacts(taskIdx int, t Task) error {
	for j, glob := range t.Artifacts {
		if glob == "" {
			return fmt.Errorf("bench.Load: task[%d].artifacts[%d]: empty: %w", taskIdx, j, ErrArtifactGlob)
		}
		if prefix := nonGlobPrefix(glob); prefix != "" && !filepath.IsLocal(prefix) {
			return fmt.Errorf("bench.Load: task[%d].artifacts[%d] %q: %w", taskIdx, j, glob, ErrArtifactGlob)
		}
	}
	return nil
}

func nonGlobPrefix(pattern string) string {
	if i := strings.IndexAny(pattern, `*?[`); i >= 0 {
		return pattern[:i]
	}
	return pattern
}

func validateCheckpoints(taskIdx int, t Task) error {
	seen := make(map[string]struct{}, len(t.Checkpoints))
	for j, name := range t.Checkpoints {
		if name == "" {
			return fmt.Errorf("bench.Load: task[%d].checkpoints[%d]: empty: %w", taskIdx, j, ErrCheckpoint)
		}
		if len(name) > maxLabelValueLen {
			return fmt.Errorf("bench.Load: task[%d].checkpoints[%d]: exceeds 256 bytes: %w", taskIdx, j, ErrCheckpoint)
		}
		if !checkpointCharset.MatchString(name) {
			return fmt.Errorf("bench.Load: task[%d].checkpoints[%d] %q: charset: %w", taskIdx, j, name, ErrCheckpoint)
		}
		if name == taskMarkerPrefix+t.ID {
			return fmt.Errorf("bench.Load: task[%d].checkpoints[%d] %q: reserved marker: %w", taskIdx, j, name, ErrCheckpoint)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("bench.Load: task[%d].checkpoints[%d] %q: duplicate: %w", taskIdx, j, name, ErrCheckpoint)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateVariants(variants []Variant) error {
	seen := make(map[string]struct{}, len(variants))
	for i, v := range variants {
		if err := checkID("variant", i, v.ID, seen); err != nil {
			return err
		}
		if v.Workspace != nil && len(v.Workspace.Cmd) == 0 {
			return fmt.Errorf("bench.Load: variant[%d].workspace.cmd: %w", i, ErrWorkspaceCmd)
		}
	}
	return nil
}

func validateWorkspaceDirExclusion(b Basket) error {
	for _, v := range b.Variants {
		if v.Workspace == nil {
			continue
		}
		for _, t := range b.Tasks {
			if t.Dir != "" {
				return fmt.Errorf(
					"bench.Load: task %q has dir but variant %q has workspace: %w",
					t.ID, v.ID, ErrWorkspaceDir,
				)
			}
		}
	}
	return nil
}

func checkID(kind string, idx int, id string, seen map[string]struct{}) error {
	if id == "" {
		return fmt.Errorf("bench.Load: %s[%d].id: %w", kind, idx, ErrEmptyID)
	}
	if len(id) > maxLabelValueLen {
		return fmt.Errorf("bench.Load: %s[%d].id: %w", kind, idx, ErrIDLen)
	}
	if !idCharset.MatchString(id) {
		return fmt.Errorf("bench.Load: %s[%d].id %q: %w", kind, idx, id, ErrCharset)
	}
	if _, dup := seen[id]; dup {
		return fmt.Errorf("bench.Load: %s[%d].id %q: %w", kind, idx, id, ErrDuplicateID)
	}
	seen[id] = struct{}{}
	return nil
}

func (b Basket) Cells() []Cell {
	if b.Reps < 1 {
		return nil
	}
	cells := make([]Cell, 0, len(b.Tasks)*len(b.Variants)*b.Reps)
	for _, t := range b.Tasks {
		for _, v := range b.Variants {
			for rep := 1; rep <= b.Reps; rep++ {
				cells = append(cells, Cell{
					Task:    t,
					Variant: v,
					Rep:     rep,
					RunID:   runID(b.Name, t.ID, v.ID, rep),
					Labels: map[string]string{
						"basket":  b.Name,
						"task":    t.ID,
						"variant": v.ID,
						"rep":     strconv.Itoa(rep),
					},
				})
			}
		}
	}
	return cells
}
