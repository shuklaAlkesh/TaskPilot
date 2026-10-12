// Package tasktypes is the registry of task types. Workers register the task types they can run
// (docs/14-DECISIONS/0009-dynamic-task-registration.md); the API only accepts jobs for registered types.
package tasktypes

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Limits for registered task types.
const (
	MaxTimeoutSeconds = 86400 // 24 h, for long jobs such as model training
	maxSchemaBytes    = 64 << 10
	maxDescription    = 2000
)

var (
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$`)
	queuePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,62}$`)

	// ErrSchemaConflict: the version already exists with a different schema while jobs still use it.
	ErrSchemaConflict = errors.New("task type version already registered with a different schema")
)

// ValidQueue reports whether q is a valid queue name.
func ValidQueue(q string) bool { return queuePattern.MatchString(q) }

// Key identifies one task type version.
type Key struct {
	TaskType    string `json:"task_type"`
	TaskVersion int    `json:"task_version"`
}

func (k Key) String() string { return fmt.Sprintf("%s@%d", k.TaskType, k.TaskVersion) }

// Definition is what a worker registers for one task type version.
type Definition struct {
	Key
	Queue                 string          `json:"queue"`
	Title                 string          `json:"title"`
	Description           string          `json:"description"`
	PayloadSchema         json.RawMessage `json:"payload_schema"`
	Example               json.RawMessage `json:"example"`
	DefaultTimeoutSeconds int             `json:"default_timeout_seconds"`
	MaxTimeoutSeconds     int             `json:"max_timeout_seconds"`
	DefaultMaxAttempts    int             `json:"default_max_attempts"`
	Idempotent            bool            `json:"idempotent"`
	SideEffects           bool            `json:"side_effects"`
	TestOnly              bool            `json:"test_only"`
}

// FieldError is one validation failure, located by JSON Pointer.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// DefinitionError lists why a definition was rejected.
type DefinitionError struct {
	Key    Key
	Fields []FieldError
}

func (e *DefinitionError) Error() string {
	msgs := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		msgs[i] = f.Field + " " + f.Message
	}
	return fmt.Sprintf("invalid task type %s: %s", e.Key, strings.Join(msgs, "; "))
}

// TaskType is a registered, compiled task type.
type TaskType struct {
	Definition
	hash   []byte
	schema *jsonschema.Schema
}

// check validates everything except the schema itself.
func (d *Definition) check() []FieldError {
	var fe []FieldError
	add := func(field, msg string) { fe = append(fe, FieldError{field, msg}) }
	if !namePattern.MatchString(d.TaskType) || len(d.TaskType) > 100 {
		add("/task_type", "must be 2-4 dot-separated lowercase segments, e.g. analytics.compute_metrics")
	}
	if d.TaskVersion < 1 {
		add("/task_version", "must be >= 1")
	}
	if !ValidQueue(d.Queue) {
		add("/queue", "must match ^[a-z][a-z0-9_.-]{0,62}$")
	}
	if d.Title == "" || len(d.Title) > 100 {
		add("/title", "must be 1-100 characters")
	}
	if len(d.Description) > maxDescription {
		add("/description", "must be at most 2000 characters")
	}
	if d.DefaultTimeoutSeconds < 1 || d.DefaultTimeoutSeconds > d.MaxTimeoutSeconds || d.MaxTimeoutSeconds > MaxTimeoutSeconds {
		add("/default_timeout_seconds", "must satisfy 1 <= default <= max <= 86400")
	}
	if d.DefaultMaxAttempts < 1 || d.DefaultMaxAttempts > 10 {
		add("/default_max_attempts", "must be 1-10")
	}
	if d.SideEffects && !d.Idempotent && d.DefaultMaxAttempts != 1 {
		add("/default_max_attempts", "must be 1 for a non-idempotent task with side effects (docs/05 §7)")
	}
	if len(d.PayloadSchema) > maxSchemaBytes {
		add("/payload_schema", "must be at most 64 KiB")
	}
	return fe
}

// compile validates a definition and compiles its schema. The schema must describe a JSON object.
func compile(d Definition) (*TaskType, error) {
	fe := d.check()
	var sch *jsonschema.Schema
	if len(d.PayloadSchema) > 0 && len(fe) == 0 {
		var doc map[string]any
		if err := json.Unmarshal(d.PayloadSchema, &doc); err != nil || doc["type"] != "object" {
			fe = append(fe, FieldError{"/payload_schema", `must be a JSON Schema with "type": "object"`})
		} else {
			parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(d.PayloadSchema))
			url := fmt.Sprintf("urn:taskpilot:task-type:%s:v%d", d.TaskType, d.TaskVersion)
			c := jsonschema.NewCompiler()
			c.DefaultDraft(jsonschema.Draft2020)
			if err == nil {
				err = c.AddResource(url, parsed)
			}
			if err == nil {
				sch, err = c.Compile(url)
			}
			if err != nil {
				fe = append(fe, FieldError{"/payload_schema", "does not compile: " + err.Error()})
			}
		}
	} else if len(d.PayloadSchema) == 0 {
		fe = append(fe, FieldError{"/payload_schema", "is required"})
	}
	if len(fe) > 0 {
		return nil, &DefinitionError{Key: d.Key, Fields: fe}
	}
	t := &TaskType{Definition: d, schema: sch}
	sum := sha256.Sum256(canonical(d.PayloadSchema))
	t.hash = sum[:]
	if len(d.Example) > 0 && !bytes.Equal(bytes.TrimSpace(d.Example), []byte("null")) {
		if fields, err := t.Validate(d.Example); err != nil || len(fields) > 0 {
			return nil, &DefinitionError{Key: d.Key, Fields: []FieldError{{"/example", "does not match payload_schema"}}}
		}
	} else {
		t.Example = nil
	}
	return t, nil
}

// canonical re-encodes JSON with sorted keys, so formatting differences don't change the hash.
func canonical(raw json.RawMessage) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

// Validate checks a payload (raw JSON) against the task schema.
// It returns field errors for an invalid payload and a plain error for malformed JSON.
func (t *TaskType) Validate(payload json.RawMessage) ([]FieldError, error) {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var ve *jsonschema.ValidationError
	if err := t.schema.Validate(inst); errors.As(err, &ve) {
		var out []FieldError
		for _, u := range ve.BasicOutput().Errors {
			if u.Error != nil {
				out = append(out, FieldError{Field: "/payload" + u.InstanceLocation, Message: u.Error.String()})
			}
		}
		return out, nil
	} else if err != nil {
		return nil, fmt.Errorf("validate payload: %w", err)
	}
	return nil, nil
}

// Registry is an in-memory snapshot of the task_types table, refreshed periodically so every
// API replica sees registrations made through any other replica.
type Registry struct {
	pool       *pgxpool.Pool
	enableTest bool

	mu    sync.RWMutex
	types map[Key]*TaskType
}

// NewRegistry builds an empty registry; call Refresh before serving.
func NewRegistry(pool *pgxpool.Pool, enableTest bool) *Registry {
	return &Registry{pool: pool, enableTest: enableTest, types: map[Key]*TaskType{}}
}

const definitionColumns = `task_type, task_version, queue, title, description, payload_schema, example,
	default_timeout_seconds, max_timeout_seconds, default_max_attempts, idempotent, side_effects, test_only`

// Refresh reloads every task type. Unchanged schemas keep their compiled form.
func (r *Registry) Refresh(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT `+definitionColumns+` FROM task_types`)
	if err != nil {
		return fmt.Errorf("load task types: %w", err)
	}
	defs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Definition, error) {
		var d Definition
		err := row.Scan(&d.TaskType, &d.TaskVersion, &d.Queue, &d.Title, &d.Description, &d.PayloadSchema, &d.Example,
			&d.DefaultTimeoutSeconds, &d.MaxTimeoutSeconds, &d.DefaultMaxAttempts, &d.Idempotent, &d.SideEffects, &d.TestOnly)
		return d, err
	})
	if err != nil {
		return fmt.Errorf("load task types: %w", err)
	}
	r.mu.RLock()
	old := r.types
	r.mu.RUnlock()
	next := make(map[Key]*TaskType, len(defs))
	for _, d := range defs {
		if d.TestOnly && !r.enableTest {
			continue
		}
		sum := sha256.Sum256(canonical(d.PayloadSchema))
		if prev, ok := old[d.Key]; ok && bytes.Equal(prev.hash, sum[:]) {
			copied := *prev
			copied.Definition = d
			if len(d.Example) == 0 || string(d.Example) == "null" {
				copied.Example = nil
			}
			next[d.Key] = &copied
			continue
		}
		t, err := compile(d)
		if err != nil {
			return fmt.Errorf("stored task type %s: %w", d.Key, err) // rows were validated on insert
		}
		next[d.Key] = t
	}
	r.mu.Lock()
	r.types = next
	r.mu.Unlock()
	return nil
}

// Run refreshes on every tick until ctx is cancelled. Failures keep the previous snapshot.
func (r *Registry) Run(ctx context.Context, interval time.Duration, onErr func(error)) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
				onErr(err)
			}
		}
	}
}

// Register validates and stores definitions from one worker, then refreshes this replica.
// Metadata (title, queue, limits) may change freely. A schema may change for an existing version
// only while no queued or running job uses that version; otherwise ErrSchemaConflict.
func (r *Registry) Register(ctx context.Context, workerID string, defs []Definition) error {
	compiled := make([]*TaskType, 0, len(defs))
	for _, d := range defs {
		t, err := compile(d)
		if err != nil {
			return err
		}
		compiled = append(compiled, t)
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for _, t := range compiled {
			var stored []byte
			err := tx.QueryRow(ctx, `SELECT schema_sha256 FROM task_types WHERE task_type = $1 AND task_version = $2 FOR UPDATE`,
				t.TaskType, t.TaskVersion).Scan(&stored)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("load %s: %w", t.Key, err)
			}
			if stored != nil && !bytes.Equal(stored, t.hash) {
				var inUse bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE task_type = $1 AND task_version = $2
					AND status IN ('queued', 'running'))`, t.TaskType, t.TaskVersion).Scan(&inUse); err != nil {
					return fmt.Errorf("check %s in use: %w", t.Key, err)
				}
				if inUse {
					return fmt.Errorf("%w: %s (bump task_version, or let its jobs finish first)", ErrSchemaConflict, t.Key)
				}
			}
			var example any
			if t.Example != nil {
				example = t.Example
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO task_types (`+definitionColumns+`, schema_sha256, registered_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (task_type, task_version) DO UPDATE SET
					queue = EXCLUDED.queue, title = EXCLUDED.title, description = EXCLUDED.description,
					payload_schema = EXCLUDED.payload_schema, example = EXCLUDED.example,
					default_timeout_seconds = EXCLUDED.default_timeout_seconds,
					max_timeout_seconds = EXCLUDED.max_timeout_seconds,
					default_max_attempts = EXCLUDED.default_max_attempts, idempotent = EXCLUDED.idempotent,
					side_effects = EXCLUDED.side_effects, test_only = EXCLUDED.test_only,
					schema_sha256 = EXCLUDED.schema_sha256, registered_by = EXCLUDED.registered_by, updated_at = now()`,
				t.TaskType, t.TaskVersion, t.Queue, t.Title, t.Description, t.PayloadSchema, example,
				t.DefaultTimeoutSeconds, t.MaxTimeoutSeconds, t.DefaultMaxAttempts, t.Idempotent, t.SideEffects, t.TestOnly,
				t.hash, workerID); err != nil {
				return fmt.Errorf("store %s: %w", t.Key, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.Refresh(ctx)
}

// Lookup returns the enabled task type for k.
func (r *Registry) Lookup(k Key) (*TaskType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.types[k]
	return t, ok
}

// List returns the enabled task types ordered by name and version.
func (r *Registry) List() []*TaskType {
	r.mu.RLock()
	out := slices.Collect(maps.Values(r.types))
	r.mu.RUnlock()
	slices.SortFunc(out, func(a, b *TaskType) int {
		return cmp.Or(cmp.Compare(a.TaskType, b.TaskType), cmp.Compare(a.TaskVersion, b.TaskVersion))
	})
	return out
}
