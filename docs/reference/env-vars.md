# env-vars

An environment-variables file holds variables shared by every app that opts in, for one target. It
lives at `capabilities/<cap>/targets/<target>/environment-variables/<name>.yml`
([ADR 0014](../adr/0014-target-environment-variables.md)). Typical uses: a feature-flag SDK key, a relay
URL, anything that differs per target but not per app.

## Defining an environment-variables file

<!-- embed path=capabilities/payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml -->
```yaml
LD_SDK_KEY: 'secret:acme-dev-onprem/launchdarkly-sdk-key'
LD_RELAY_URL: https://ld-relay.onprem-dev.example.com
APP_ENV: 'from-bundle'
```

Source: [`payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml`](../../platform/render/testdata/root/capabilities/payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml)
<!-- /embed -->

The file is a flat map of `KEY: value`, no nesting. Values are strings, or
`secret:<gcp-project>/<secret>[@<version>]` to reference a Secret Manager secret, exactly as in an app's
`env`.

## Using it from an app

An app includes the file by its `<name>` in `envFrom`, at any layer of its `app.yml`:

<!-- embed path=capabilities/payments/apps/my-app/app.yml lines=9-11 -->
```yaml
stages:
  dev:
    envFrom: [launchdarkly]  # environment-variables files of the target, merged under env
```

Source: [`payments/apps/my-app/app.yml:9-11`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml#L9-L11)
<!-- /embed -->

- Every target the setting reaches must have that file; set `envFrom` at the app level only if all of
  the app's targets do.
- `envFrom` lists add up across layers: a narrower layer adds files, it never removes one.
- Keys in the app's `env` win over keys from `envFrom`. `my-app` also sets `APP_ENV` in its `env`, so
  the file's `APP_ENV` is ignored for it.

The name must be lowercase letters, digits and `-`, starting with a letter, up to 40 characters.

Every field: `gcd schema env-vars`.
