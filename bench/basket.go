package bench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

const maxLabelValueLen = 256

var (
	ErrEmptyBasketName = errors.New("bench: basket name is empty")
	ErrBasketNameLen   = errors.New("bench: basket name exceeds 256 bytes")
	ErrReps            = errors.New("bench: reps must be >= 1")
	ErrNoTasks         = errors.New("bench: at least one task is required")
	ErrNoVariants      = errors.New("bench: at least one variant is required")
	ErrEmptyID         = errors.New("bench: id is empty")
	ErrIDLen           = errors.New("bench: id exceeds 256 bytes")
	ErrDuplicateID     = errors.New("bench: duplicate id")
	ErrEmptyCmd        = errors.New("bench: task cmd is empty")
)

type Task struct {
	ID  string            `yaml:"id" json:"id"`
	Cmd []string          `yaml:"cmd" json:"cmd"`
	Dir string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

type Variant struct {
	ID    string            `yaml:"id" json:"id"`
	Env   map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Setup []string          `yaml:"setup,omitempty" json:"setup,omitempty"`
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

func Load(path string) (Basket, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Basket{}, "", fmt.Errorf("bench.Load: %w", err)
	}
	var b Basket
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&b); err != nil {
		return Basket{}, "", fmt.Errorf("bench.Load: %w", err)
	}
	if err := validate(b); err != nil {
		return Basket{}, "", err
	}
	sum := sha256.Sum256(data)
	return b, hex.EncodeToString(sum[:]), nil
}

func validate(b Basket) error {
	if b.Name == "" {
		return fmt.Errorf("bench.Load: basket: %w", ErrEmptyBasketName)
	}
	if len(b.Name) > maxLabelValueLen {
		return fmt.Errorf("bench.Load: basket: %w", ErrBasketNameLen)
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
	return validateVariants(b.Variants)
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
	}
	return nil
}

func validateVariants(variants []Variant) error {
	seen := make(map[string]struct{}, len(variants))
	for i, v := range variants {
		if err := checkID("variant", i, v.ID, seen); err != nil {
			return err
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
	if _, dup := seen[id]; dup {
		return fmt.Errorf("bench.Load: %s[%d].id %q: %w", kind, idx, id, ErrDuplicateID)
	}
	seen[id] = struct{}{}
	return nil
}

func (b Basket) Cells() []Cell {
	cells := make([]Cell, 0, len(b.Tasks)*len(b.Variants)*b.Reps)
	for _, t := range b.Tasks {
		for _, v := range b.Variants {
			for rep := 1; rep <= b.Reps; rep++ {
				cells = append(cells, Cell{
					Task:    t,
					Variant: v,
					Rep:     rep,
					RunID:   fmt.Sprintf("bench-%s-%s-%s-r%d", b.Name, t.ID, v.ID, rep),
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
