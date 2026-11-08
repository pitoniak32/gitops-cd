#!/usr/bin/env bash
# Generate editor JSON Schemas (generic: stage/target names are a pattern, not the
# team's names, so teams never regenerate). Output is committed to schemas/ and
# referenced by team YAML via relative $schema paths.
# usage: schemas.sh [out-dir]   (default: schemas/)
set -euo pipefail
cd "$(dirname "$0")/.."
OUT="${1:-schemas}"; mkdir -p "$OUT"
for def in App Stage Target EnvironmentVariables Owners Team; do
  f="$OUT/$(sed -E 's/([a-z])([A-Z])/\1-\2/g' <<<"$def" | tr '[:upper:]' '[:lower:]').json"
  cue def . -t generic=true -e "#$def" --out jsonschema |
  # CUE embedding exports as {$ref, properties, additionalProperties:false}, which JSON Schema
  # treats as two closed schemas rejecting each other's keys. Inline the ref'd properties.
  # error("...") branches (custom CUE messages) export as `false`; drop them from anyOf
  jq 'walk(if type == "object" and has("anyOf") then .anyOf |= map(select(. != false)) else . end)
  | .["$defs"] as $d | walk(
    if type == "object" and has("$ref") and has("properties") and (.["$ref"] | startswith("#/$defs/"))
    then (.properties = ($d[.["$ref"] | ltrimstr("#/$defs/")].properties + .properties)) | del(.["$ref"])
    else . end)
  # "Property x is not allowed" -> say what is allowed.
  | walk(
    if type == "object" and .additionalProperties == false and has("properties") then
      (.properties | keys) as $k
      | del(.additionalProperties)
      | .propertyNames = {enum: $k, errorMessage: ("Unknown key. Allowed: " + ($k | join(", ")))}
    elif type == "object" and .additionalProperties == false and has("patternProperties") then
      (.patternProperties | keys | join(" or ")) as $p
      | del(.additionalProperties)
      | .propertyNames = {pattern: $p, errorMessage: ("Invalid name, must match " + $p)}
    else . end)
  # yamlls ignores errorMessage on `not`; fold not:{pattern} into one lookahead pattern
  | walk(
    if type == "object" and (.not? | type) == "object" and (.not | keys) == ["pattern"] then
      .pattern = ("^(?![\\s\\S]*?(?:" + .not.pattern + "))" + (if .pattern then "[\\s\\S]*?(?:" + .pattern + ")" else "" end))
      | del(.not)
    else . end)
  # readable messages for pattern-heavy fields
  | walk(
    if type == "object" and (.pattern? // "" | contains("(/[a-z0-9._-]+)+$")) then
      .errorMessage = "Image repository only, no tag or digest (Kargo discovers SemVer tags)"
    elif type == "object" and (.pattern? // "" | test("^\\^\\[a-z0-9._%\\+-\\]\\+@")) then
      .errorMessage = ("Lowercase email in the allowed domain: " + (.pattern | sub("^.*@"; "") | sub("\\$$"; "") | gsub("\\\\"; "")))
    elif type == "object" and has("anyOf") and (.description? // "" | startswith("Plain value, or a Secret")) then
      .errorMessage = "Plain string, or secret:<gcp-project>/<secret>[@<version>]"
    else . end)
  # yamlls reports pattern failures (incl. inside anyOf branches) via patternErrorMessage
  | walk(
    if type == "object" and has("errorMessage") and (has("pattern") or has("anyOf")) then
      .errorMessage as $m | .patternErrorMessage = $m
      | if has("anyOf") then .anyOf |= map(. + {patternErrorMessage: $m}) else . end
    else . end)' >"$f"
  echo "wrote $f"
done
