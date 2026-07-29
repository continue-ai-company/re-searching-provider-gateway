# Re-Searching Provider Gateway

This fork builds a private, loopback-only structured inference sidecar for
Re-Searching. It is not a general proxy product and is not intended to be run
as a shared network service.

The first release enables only the `codex` provider. The provider allowlist is
part of the process contract so future providers can be added deliberately,
with their own credential policy and acceptance tests, without changing the
Re-Searching domain contract.

## Runtime contract

Build the dedicated command:

```bash
go build -o re-searching-provider-gateway ./cmd/re-searching-gateway
```

Start it with a private configuration file and an absolute ready descriptor:

```bash
re-searching-provider-gateway \
  --config /private/runtime/config.yaml \
  --ready-descriptor /private/runtime/ready.json \
  --enabled-providers codex
```

The configuration file must be a regular, non-symlink file with owner-only
permissions. Its `auth-dir` must already exist as an owner-only, non-symlink
directory. Re-Searching creates both resources and provides one random client
bearer key.

The restricted command:

- forces `127.0.0.1` and accepts `port: 0`;
- exposes only `GET/HEAD /healthz`, authenticated
  `GET /rs/v1/capabilities`, `GET /v1/models`, and
  `POST /v1/responses`;
- enables only allowlisted credentials and executors;
- disables management, UI, OAuth callbacks, plugins, Home, pprof, file request
  logs, usage dispatch, websocket ingress, automatic retries, credential
  auto-refresh, and remote model/update loops;
- writes the ready descriptor only after the listener is bound;
- adds `X-Re-Searching-Gateway-Version` and
  `X-Re-Searching-Gateway-Protocol-Version` response headers.

The ready descriptor is a private transport handoff. It contains the protocol
version, build, enabled providers, PID, loopback host, assigned port, and
timestamp. It never contains the bearer key or a credential path.

`GET /rs/v1/capabilities` is the provider-neutral readiness contract. Its
per-provider `models` array is intentionally a strict subset of `/v1/models`:
it contains only models available to the current account that expose at least
one reasoning effort and have a matching positive priority in the provider
catalog. Each entry has `id`, `priority`, `reasoning_efforts`, the optional
`default_service_tier`, and its validated `service_tiers`; entries are ordered
by ascending priority and then model ID. A missing catalog match, non-positive
priority, or empty reasoning-effort list is omitted rather than guessed.
Re-Searching freezes an exact model, reasoning effort, and service tier from
this fail-closed catalog.

## Provenance and releases

[`upstream.lock.json`](upstream.lock.json) pins the upstream CLIProxyAPI tag and
commit, Go toolchain, gateway protocol, and build target. Re-Searching runtime
code must pin a released archive and checksum; it must not download or update
the gateway at runtime.

The release workflow builds only macOS arm64, writes SHA-256 checksums, records
Go build metadata, and generates an SPDX JSON SBOM with Syft. The archive also
contains the upstream MIT license and
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

For a local inventory check:

```bash
go version -m ./re-searching-provider-gateway
go list -m all
```
