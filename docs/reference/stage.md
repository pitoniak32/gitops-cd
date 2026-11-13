# stage

A stage is one step of a capability's promotion pipeline, such as `dev` or `prod`. Each is a file
`capabilities/<cap>/stages/<stage>.yml`, and the file name is the stage name. Together the files form a
graph that every app of the capability follows ([ADR 0012](../adr/0012-kargo-stage-per-target.md)).
`owners` is reserved and can't be a stage name.

How-to: [targets and stages](../guide/targets-stages.md#stages-the-pipeline).

## Defining a stage

<!-- embed path=capabilities/payments/stages/perf.yml -->
```yaml
dependsOn: [dev]
color: teal  # Kargo UI color; default is picked per stage
```

Source: [`payments/stages/perf.yml`](../../platform/render/testdata/root/capabilities/payments/stages/perf.yml)
<!-- /embed -->

All fields are optional.

## Dependencies

`dependsOn` lists the stages whose verified Freight this stage takes. A stage with none is a starting
point. Cycles are rejected. The sandbox pipeline is `dev → staging, perf → prod`:

<!-- embed path=capabilities/payments/stages/prod.yml -->
```yaml
dependsOn: [staging, perf]
```

Source: [`payments/stages/prod.yml`](../../platform/render/testdata/root/capabilities/payments/stages/prod.yml)
<!-- /embed -->

Each target an app deploys to in a stage is its own Kargo Stage. Where several targets fan in or out,
the platform adds control-flow Stages (`<stage>-done`, `<stage>`) that need no action.

## Auto-promotion

`autoPromote: true` promotes Freight automatically once all `dependsOn` stages have verified it.

<!-- embed path=capabilities/payments/stages/dev.yml -->
```yaml
dependsOn: []
autoPromote: true
```

Source: [`payments/stages/dev.yml`](../../platform/render/testdata/root/capabilities/payments/stages/dev.yml)
<!-- /embed -->

Prod is always promoted by a person: a stage that deploys to any `prd` target can't auto-promote.

```yaml path=capabilities/payments/stages/prod.yml expect="autoPromote not allowed"
dependsOn: [staging, perf]
autoPromote: true
```

## Color

`color` sets the Kargo UI color of the stage's Kargo Stages; without it one is picked per stage. One of
`red`, `salmon`, `orange`, `amber`, `yellow`, `lime`, `avocado`, `green`, `teal`, `cyan`, `sky`,
`blue`, `indigo`, `periwinkle`, `violet`, `purple`, `fuchsia`, `pink`, `rose`, `dragonfruit`, `gray`.

Every field: `gcd schema stage`.
