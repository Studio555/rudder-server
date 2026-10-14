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

Build and publish locally from the reviewed maintenance-branch commit. The IaC image publishers were removed in `s555-iac#100`; no publisher resources or GitHub image variables are required for this path.

```sh
RUDDER_SHA="$(git rev-parse HEAD)"
RUDDER_IMAGE="europe-north1-docker.pkg.dev/s555-devservices-shared/docker/rudder-server:$RUDDER_SHA"
docker buildx build --platform linux/amd64 --load --build-arg VERSION=1.90.0-studio555 --build-arg COMMIT_HASH="$RUDDER_SHA" --build-arg REVISION="$RUDDER_SHA" -t "$RUDDER_IMAGE" .
docker run --rm --entrypoint /bin/sh "$RUDDER_IMAGE" -c 'test -x /rudder-server && pg_isready --version && psql --version'
docker push "$RUDDER_IMAGE"
gcloud artifacts docker images describe "$RUDDER_IMAGE" --format='value(image_summary.digest)'
```

Promote the published digest together with the tested transformer digest in `Pulumi.rudderstack.yaml`, enable `preserveIntegerPrecision`, and inspect `pulumi preview -s rudderstack --non-interactive --diff`. Apply only after the isolated canary passes. Existing rounded jobs and warehouse rows are not repaired by this patch.

The upstream `make lint` command includes actionlint checks on inherited workflows that currently fail shellcheck. Studio555 CI runs the complete Go lint check and actionlint on its own workflow separately. Security scans remain required.

## Inherited security baseline

The fork uses Go 1.27.2 and x/net 0.60.0, removing the twelve fixable advisories reported for Go 1.27.1 / x/net 0.58.0. Both Docker builder digests match Go 1.27.2.

As of 2026-10-14, `govulncheck` still reports four advisories without published fixes: GO-2026-6665 in Snowflake, and GO-2026-5046/5047/5048 in the Avro decoder. The live workspace enables only BigQuery destinations; its loaders use JSON, so the affected integrations are not used by the current event flow. This is a configuration-specific exposure assessment, not a clean security scan or removal of the vulnerable code. Reassess before enabling Snowflake or Avro input. CI retains the scan as a report; precision tests, Go lint, workflow lint and changed-commit secret scanning remain blocking.
