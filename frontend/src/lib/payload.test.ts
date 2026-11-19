import { describe, expect, it } from "vitest";

import { FIELD_PREFIX, fieldsFromSchema, payloadFromForm } from "./payload";

const csvSchema = {
  type: "object",
  required: ["csv"],
  properties: {
    csv: { type: "string", maxLength: 60000 },
    has_header: { type: "boolean", default: true },
    delimiter: { type: "string", enum: [",", ";"], default: "," },
    limit: { type: "integer" },
  },
};

describe("fieldsFromSchema", () => {
  it("maps simple properties to form fields", () => {
    expect(fieldsFromSchema(csvSchema)).toEqual([
      { name: "csv", kind: "text", required: true },
      { name: "has_header", kind: "boolean", required: false, defaultValue: true },
      { name: "delimiter", kind: "enum", required: false, options: [",", ";"], defaultValue: "," },
      { name: "limit", kind: "integer", required: false },
    ]);
  });

  it("falls back to raw JSON for nested schemas", () => {
    expect(fieldsFromSchema({ type: "object", properties: { nested: { type: "object" } } })).toBeNull();
    expect(fieldsFromSchema(null)).toBeNull();
  });
});

describe("payloadFromForm", () => {
  it("coerces values and omits empty optional fields", () => {
    const form = new FormData();
    form.set(`${FIELD_PREFIX}csv`, "a,b\n1,2");
    form.set(`${FIELD_PREFIX}delimiter`, ";");
    form.set(`${FIELD_PREFIX}limit`, "");
    expect(payloadFromForm(fieldsFromSchema(csvSchema) ?? [], form)).toEqual({
      csv: "a,b\n1,2",
      has_header: false,
      delimiter: ";",
    });
  });
});
