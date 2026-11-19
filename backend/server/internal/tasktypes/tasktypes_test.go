package tasktypes

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func echoDefinition() Definition {
	return Definition{
		Key:   Key{"system.echo", 1},
		Queue: "default", Title: "Echo", Description: "Returns the message.",
		PayloadSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["message"],
			"properties":{"message":{"type":"string","minLength":1,"maxLength":1000}}}`),
		Example:               json.RawMessage(`{"message":"hi"}`),
		DefaultTimeoutSeconds: 10, MaxTimeoutSeconds: 30, DefaultMaxAttempts: 3, Idempotent: true,
	}
}

func TestCompile_ValidDefinition(t *testing.T) {
	t.Parallel()
	tt, err := compile(echoDefinition())
	require.NoError(t, err)

	fields, err := tt.Validate(json.RawMessage(`{"message":"hello"}`))
	require.NoError(t, err)
	require.Empty(t, fields)

	fields, err = tt.Validate(json.RawMessage(`{"message":"","extra":1}`))
	require.NoError(t, err)
	require.NotEmpty(t, fields)
	for _, f := range fields {
		require.Regexp(t, `^/payload`, f.Field)
	}
}

func TestCompile_RejectsBadDefinitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Definition)
		field  string
	}{
		{"name with one segment", func(d *Definition) { d.TaskType = "echo" }, "/task_type"},
		{"name with five segments", func(d *Definition) { d.TaskType = "a.b.c.d.e" }, "/task_type"},
		{"uppercase queue", func(d *Definition) { d.Queue = "Analytics" }, "/queue"},
		{"timeout above 24 h", func(d *Definition) { d.MaxTimeoutSeconds = 86401 }, "/default_timeout_seconds"},
		{"default above max", func(d *Definition) { d.DefaultTimeoutSeconds = 60 }, "/default_timeout_seconds"},
		{"retrying a non-idempotent side effect", func(d *Definition) { d.SideEffects, d.Idempotent = true, false }, "/default_max_attempts"},
		{"schema is not an object", func(d *Definition) { d.PayloadSchema = json.RawMessage(`{"type":"string"}`) }, "/payload_schema"},
		{"schema does not compile", func(d *Definition) { d.PayloadSchema = json.RawMessage(`{"type":"object","minProperties":"x"}`) }, "/payload_schema"},
		{"example does not validate", func(d *Definition) { d.Example = json.RawMessage(`{"message":""}`) }, "/example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := echoDefinition()
			tt.mutate(&d)
			_, err := compile(d)
			var de *DefinitionError
			require.True(t, errors.As(err, &de), "expected DefinitionError, got %v", err)
			require.Contains(t, de.Error(), tt.field)
		})
	}
}

func TestCompile_AcceptsDottedNamesAndLongTimeouts(t *testing.T) {
	t.Parallel()
	d := echoDefinition()
	d.TaskType, d.Queue = "synthetic_data.tasks.generate_synthetic_data", "image_generation"
	d.DefaultTimeoutSeconds, d.MaxTimeoutSeconds = 3600, 86400
	_, err := compile(d)
	require.NoError(t, err)
}

func TestCompile_HashIgnoresFormatting(t *testing.T) {
	t.Parallel()
	a, err := compile(echoDefinition())
	require.NoError(t, err)
	d := echoDefinition()
	d.PayloadSchema = json.RawMessage(`{"properties":{"message":{"maxLength":1000,"minLength":1,"type":"string"}},
		"required":["message"],"additionalProperties":false,"type":"object"}`)
	b, err := compile(d)
	require.NoError(t, err)
	require.Equal(t, a.hash, b.hash)
}
