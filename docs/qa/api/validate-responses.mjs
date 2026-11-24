// Validate recorded endpoint responses against openapi.yaml (issue #697).
//
// Usage:
//   node docs/qa/api/validate-responses.mjs
//
// Reads openapi.yaml from the repository root and validates all JSON responses
// recorded in docs/qa/api/results/endpoints.json against the OpenAPI 3.0 schemas.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { repoRoot, resultsDir } from "./lib.mjs";

const openApiPath = join(repoRoot, "openapi.yaml");
const endpointsPath = join(resultsDir, "endpoints.json");

function parseYamlSubset(yamlText) {
  const lines = yamlText.split("\n");
  const parseBlock = (startLine, minIndent) => {
    let m = null;
    let l = null;
    let i = startLine;

    while (i < lines.length) {
      const raw = lines[i];
      const trimmed = raw.trim();
      if (!trimmed || trimmed.startsWith("#")) {
        i++;
        continue;
      }

      const indent = raw.length - raw.trimStart().length;
      if (indent < minIndent) break;

      const lineContent = stripComment(trimmed);

      if (lineContent.startsWith("- ")) {
        if (m) throw new Error(`line ${i + 1}: mixed list and map`);
        if (!l) l = [];

        const itemContent = lineContent.slice(2).trim();
        if (!itemContent) {
          const child = parseBlock(i + 1, indent + 1);
          l.push(child);
          i = nextLineAfter(lines, i + 1, indent + 1);
          continue;
        } else if (itemContent.includes(": ") || itemContent.endsWith(":")) {
          const colonIdx = itemContent.indexOf(":");
          const firstKey = itemContent.slice(0, colonIdx).trim();
          const firstVal = itemContent.slice(colonIdx + 1).trim();

          const subMap = {};
          if (firstVal) {
            subMap[firstKey] = parseScalar(firstVal);
          } else {
            subMap[firstKey] = parseBlock(i + 1, indent + firstKey.length + 4);
          }

          let j = i + 1;
          const subLines = [];
          while (j < lines.length) {
            const jRaw = lines[j];
            const jTrim = jRaw.trim();
            if (!jTrim || jTrim.startsWith("#")) {
              j++;
              continue;
            }
            const jIndent = jRaw.length - jRaw.trimStart().length;
            if (jIndent <= indent || (jIndent === indent + 2 && jTrim.startsWith("- "))) {
              break;
            }
            subLines.push(jRaw);
            j++;
          }

          if (subLines.length > 0) {
            const tempParser = parseYamlSubset(subLines.join("\n"));
            Object.assign(subMap, tempParser);
          }
          l.push(subMap);
          i = j;
          continue;
        } else {
          l.push(parseScalar(itemContent));
          i++;
          continue;
        }
      }

      if (l) throw new Error(`line ${i + 1}: mixed list and map`);
      if (!m) m = {};

      const colonIdx = lineContent.indexOf(":");
      if (colonIdx === -1) {
        throw new Error(`line ${i + 1}: expected key-value pair, got ${lineContent}`);
      }

      const key = unquote(lineContent.slice(0, colonIdx).trim());
      const valStr = lineContent.slice(colonIdx + 1).trim();

      if (!valStr) {
        m[key] = parseBlock(i + 1, indent + 1);
        i = nextLineAfter(lines, i + 1, indent + 1);
      } else {
        m[key] = parseScalar(valStr);
        i++;
      }
    }

    return l || m || {};
  };

  return parseBlock(0, 0);
}

function nextLineAfter(lines, startLine, minIndent) {
  for (let i = startLine; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const indent = raw.length - raw.trimStart().length;
    if (indent < minIndent) return i;
  }
  return lines.length;
}

function stripComment(s) {
  let inQuote = false;
  let qChar = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (inQuote) {
      if (c === qChar) inQuote = false;
    } else {
      if (c === '"' || c === "'") {
        inQuote = true;
        qChar = c;
      } else if (c === "#" && i > 0 && s[i - 1] === " ") {
        return s.slice(0, i).trim();
      }
    }
  }
  return s;
}

function parseScalar(s) {
  s = s.trim();
  if (s === "true") return true;
  if (s === "false") return false;
  if (s === "null" || s === "~") return null;
  if (s.startsWith("[") && s.endsWith("]")) {
    const inner = s.slice(1, -1).trim();
    if (!inner) return [];
    return inner.split(",").map((p) => parseScalar(p));
  }
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    return unquote(s);
  }
  if (/^-?\d+$/.test(s)) return parseInt(s, 10);
  if (/^-?\d+\.\d+$/.test(s)) return parseFloat(s);
  return s;
}

function unquote(s) {
  s = s.trim();
  if (s.length >= 2 && ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'")))) {
    return s.slice(1, -1);
  }
  return s;
}

class OpenAPIValidator {
  constructor(doc) {
    this.schemas = (doc.components && doc.components.schemas) || {};
    this.paths = doc.paths || {};
  }

  getSchemaFor(method, path, status) {
    const pathItem = this.paths[path];
    if (!pathItem) return null;
    const op = pathItem[method.toLowerCase()];
    if (!op || !op.responses) return null;
    const resp = op.responses[String(status)] || op.responses.default;
    if (!resp || !resp.content || !resp.content["application/json"]) return null;
    return resp.content["application/json"].schema;
  }

  validate(schema, value, path = "") {
    const errors = [];
    const at = (msg) => errors.push(path ? `${path}: ${msg}` : msg);

    if (value === null || value === undefined) {
      if (schema && schema.nullable) return [];
      at("unexpected null value");
      return errors;
    }

    if (schema.$ref) {
      const prefix = "#/components/schemas/";
      if (schema.$ref.startsWith(prefix)) {
        const name = schema.$ref.slice(prefix.length);
        const target = this.schemas[name];
        if (!target) {
          at(`unresolvable $ref ${schema.$ref}`);
          return errors;
        }
        return this.validate(target, value, path);
      }
    }

    if (schema.type) {
      const actualType = Array.isArray(value) ? "array" : typeof value;
      if (schema.type === "integer") {
        if (typeof value !== "number" || !Number.isInteger(value)) {
          at(`want type integer, got ${actualType}`);
          return errors;
        }
      } else if (schema.type === "number") {
        if (typeof value !== "number") {
          at(`want type number, got ${actualType}`);
          return errors;
        }
      } else if (actualType !== schema.type) {
        at(`want type ${schema.type}, got ${actualType}`);
        return errors;
      }
    }

    if (schema.enum && !schema.enum.includes(value)) {
      at(`value ${value} is not in enum [${schema.enum.join(", ")}]`);
    }

    if (schema.pattern && typeof value === "string") {
      const re = new RegExp(schema.pattern);
      if (!re.test(value)) {
        at(`"${value}" does not match pattern ${schema.pattern}`);
      }
    }

    if (typeof value === "object" && !Array.isArray(value)) {
      if (schema.required) {
        for (const req of schema.required) {
          if (!(req in value)) {
            const fieldPath = path ? `${path}.${req}` : req;
            errors.push(`${fieldPath}: required field is missing`);
          }
        }
      }
      if (schema.properties) {
        for (const [k, val] of Object.entries(value)) {
          const fieldPath = path ? `${path}.${k}` : k;
          if (schema.properties[k]) {
            errors.push(...this.validate(schema.properties[k], val, fieldPath));
          } else if (schema.additionalProperties === false) {
            errors.push(`${fieldPath}: unexpected property not in schema`);
          } else if (typeof schema.additionalProperties === "object") {
            errors.push(...this.validate(schema.additionalProperties, val, fieldPath));
          }
        }
      }
    }

    if (Array.isArray(value) && schema.items) {
      for (let i = 0; i < value.length; i++) {
        errors.push(...this.validate(schema.items, value[i], `${path}[${i}]`));
      }
    }

    return errors;
  }
}

function main() {
  const yamlRaw = readFileSync(openApiPath, "utf8");
  const doc = parseYamlSubset(yamlRaw);
  const validator = new OpenAPIValidator(doc);

  const endpointsRaw = readFileSync(endpointsPath, "utf8");
  const data = JSON.parse(endpointsRaw);

  let checked = 0;
  let failures = 0;

  for (const c of data.cases) {
    if (c.path === "/" || c.status === 204) continue;
    const basePath = c.path.split("?")[0];
    const schema = validator.getSchemaFor(c.method, basePath, c.status);
    if (!schema) {
      if (c.method !== "GET" && (c.status === 200 || c.status === 405)) continue;
      console.error(`FAIL: ${c.name} (${c.method} ${c.path} -> ${c.status}): schema not found in openapi.yaml`);
      failures++;
      continue;
    }

    let parsedBody;
    try {
      parsedBody = JSON.parse(c.body);
    } catch (err) {
      console.error(`FAIL: ${c.name}: invalid JSON body: ${err.message}`);
      failures++;
      continue;
    }

    const errs = validator.validate(schema, parsedBody);
    if (errs.length > 0) {
      console.error(`FAIL: ${c.name} (${c.method} ${c.path} -> ${c.status}):\n  ${errs.join("\n  ")}`);
      failures++;
    } else {
      checked++;
    }
  }

  console.log(`Validated ${checked} recorded endpoint responses against openapi.yaml.`);
  if (failures > 0) {
    console.error(`Validation failed with ${failures} error(s).`);
    process.exit(1);
  }
  console.log("All recorded responses match openapi.yaml.");
}

main();
