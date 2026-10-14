# Studio555 integer precision

This fork is based on upstream `v1.90.0` (`9b06b87c9c0560fc5271d1872a5bc572cd3c1d62`). The previously running production backend was `1.89.2`; this release therefore includes an upstream version change as well as the precision patch.

Set `Processor.preserveIntegerPrecision: true` in the server configuration and `PRESERVE_INTEGER_PRECISION=true` in the Studio555 transformer. Both default to false. Restart both services when changing the setting.

The enabled path preserves numeric JSON tokens in gateway ingestion, processor jobs, transformer responses, warehouse transformation and staging-file decoding. The loader writes checked `int64` values. An internal JSON marshaler keeps numbers numeric with std, Sonnet and jsoniter. Fractions retain floating-point behavior; integral decimal/exponent forms are classified exactly. Out-of-range integers fail per event instead of wrapping or changing to strings.

The transformer uses `BigInt` for integers outside JavaScript's safe range and emits numeric JSON tokens. User transformations must retain these values as BigInt; `Number(value)` can lose precision. The active Studio555 transformation removes IP addresses and does not perform numeric conversions.

## Isolated BigQuery canary

```sh
python3 .studio555/precision-canary.py \
  --server-image SERVER_IMAGE \
  --transformer-image TRANSFORMER_IMAGE
```

Requires Docker Compose, `bq`, and existing Application Default Credentials with write access to `s555-data.ingress_nonprod` and `rudderstack-staging-nonprod`. The credentials are mounted read-only; no service account key is created or committed. Runtime configuration is generated under the user's cache directory so Colima can mount it.

The script creates an isolated PostgreSQL jobsdb, a local transformation-code service, the transformer and the backend. It sends four synthetic events through the IP-removal transformation and compares BigQuery values using Python integers. It writes only a uniquely named canary table in `ingress_nonprod`; tracks/users tables are skipped. Containers and local jobsdb volumes are removed afterward. Synthetic BigQuery evidence is retained. Production endpoints, jobsdb and control-plane configuration are not changed.

## Build and promotion

The `studio555-ci.yml` workflow verifies PRs targeting `studio555/v1.90.0`, builds and smoke-tests an amd64 image, and publishes that same image after a maintenance-branch push. Image tags are full source commit SHAs. It does not deploy infrastructure.

The operator first provisions the OIDC publisher from `s555-iac` and sets these repository variables from the infra stack outputs:

- `GCP_IMAGE_WORKLOAD_IDENTITY_PROVIDER`: `rudderServerImagePublisherWorkloadIdentityProvider`
- `GCP_IMAGE_PUBLISHER_SERVICE_ACCOUNT`: `rudderServerImagePublisherServiceAccount`

Promote the published digest together with the tested transformer digest in `Pulumi.rudderstack.yaml`, enable `preserveIntegerPrecision`, and inspect `pulumi preview -s rudderstack --non-interactive --diff`. Apply only after the isolated canary passes. Existing rounded jobs and warehouse rows are not repaired by this patch.

The upstream `make lint` command includes actionlint checks on inherited workflows that currently fail shellcheck. Studio555 CI runs the complete Go lint check and actionlint on its own workflow separately. Security scans remain required.
