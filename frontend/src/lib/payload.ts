// Builds simple payload forms from a task's JSON Schema (docs/16-FRONTEND.md §3, /jobs/new).
// Only flat objects with string/number/integer/boolean/enum properties get a generated form;
// anything else falls back to the raw JSON editor. The Go API validates the payload either way.

export type FieldKind = "string" | "text" | "number" | "integer" | "boolean" | "enum";

export type Field = {
  name: string;
  kind: FieldKind;
  required: boolean;
  options?: string[];
  defaultValue?: string | number | boolean;
  description?: string;
};

export const FIELD_PREFIX = "p.";
const LONG_TEXT = 200;

type PropertySchema = {
  type?: string;
  enum?: unknown[];
  maxLength?: number;
  default?: unknown;
  description?: string;
};

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function kindOf(p: PropertySchema): FieldKind | null {
  if (p.enum?.every((v) => typeof v === "string")) return "enum";
  switch (p.type) {
    case "string":
      return (p.maxLength ?? 0) > LONG_TEXT ? "text" : "string";
    case "number":
    case "integer":
    case "boolean":
      return p.type;
  }
  return null;
}

/** Form fields for a flat schema, or null when the schema needs the raw JSON editor. */
export function fieldsFromSchema(schema: unknown): Field[] | null {
  if (!isRecord(schema) || schema.type !== "object" || !isRecord(schema.properties)) return null;
  const required = new Set(Array.isArray(schema.required) ? schema.required : []);
  const fields: Field[] = [];
  for (const [name, raw] of Object.entries(schema.properties)) {
    if (!isRecord(raw)) return null;
    const p = raw as PropertySchema;
    const kind = kindOf(p);
    if (!kind) return null;
    const field: Field = { name, kind, required: required.has(name) };
    if (kind === "enum") field.options = p.enum as string[];
    if (["string", "number", "boolean"].includes(typeof p.default)) {
      field.defaultValue = p.default as string | number | boolean;
    }
    if (p.description) field.description = p.description;
    fields.push(field);
  }
  return fields;
}

/** Reads generated form fields back into a payload object. Empty optional fields are omitted. */
export function payloadFromForm(fields: Field[], form: FormData): Record<string, unknown> {
  const payload: Record<string, unknown> = {};
  for (const f of fields) {
    const key = FIELD_PREFIX + f.name;
    if (f.kind === "boolean") {
      payload[f.name] = form.has(key);
      continue;
    }
    const value = String(form.get(key) ?? "");
    if (value === "" && !f.required) continue;
    payload[f.name] = f.kind === "number" || f.kind === "integer" ? Number(value) : value;
  }
  return payload;
}
