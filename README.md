# hytale-runner

Runs a Hytale server, using an **OCI registry** as the state store instead of the
filesystem. On each run it pulls the saved world/state from the registry, runs
the server, then pushes the state back.

```
pull state (OCI)  ->  run server  ->  push state (OCI)
```

> ⚠️ Early work in progress.

## How it works

- **Immutable bits** (the server jar, `Assets.zip`) come from disk.
- **Mutable state** (worlds, configs under `--data-dir`) is the OCI round-trip,
  stored as an artifact at `<registry>/<state-repo>:<state-tag>`.
- First run (no stored state) starts fresh; the server's exit code is preserved
  (incl. `8` = restart-for-update). SIGINT/SIGTERM are forwarded for a graceful
  shutdown so state is still saved.

## Quick start (dev container)

A `docker-compose.yml` brings up a local [zot](https://zotregistry.dev) registry
plus a Go dev container.

```sh
make dev-up       # start registry + dev container
make dev-shell    # shell into the dev container
make run          # pull state, run the server, push state
```

For commit signing inside the container, copy the override template:
`cp docker-compose.override.yml.example docker-compose.override.yml`.

## Commands

```sh
hytale-runner run            # one-shot: pull state -> run server -> push state
hytale-runner serve          # resident: serve AWS MicroVM lifecycle hooks
hytale-runner state pull     # pull state into the data dir
hytale-runner state push     # push the data dir as state
hytale-runner version
```

## AWS Lambda MicroVM runtime (`serve`)

`serve` turns the runner into a resident runtime for [AWS Lambda MicroVMs][mvm].
Instead of the one-shot flow, it stays up for the MicroVM's whole life and drives
the server from HTTP lifecycle hooks that Lambda posts to
`/aws/lambda-microvms/runtime/v1/<hook>` on `--hook-port` (default `8080`):

| Hook | Runner action |
|------|---------------|
| `run` | pull state from OCI, then start the server (traffic begins after 200) |
| `suspend` / `resume` | record the transition (disk+memory are checkpointed by Lambda) |
| `terminate` | stop the server gracefully (`--terminate-grace`), then push state to OCI |
| `ready` | build-time snapshot gate (200 once the hook server is listening) |

There is also `GET /health` (200 while the game process is alive, else 503).
Hooks are unauthenticated at the app: Lambda's proxy terminates TLS and enforces
JWE auth before they reach the runner. State is durable across MicroVMs via the
OCI round-trip — pulled on `run`, pushed on `terminate`.

### Per-MicroVM JVM overrides

The `run-microvm --run-hook-payload` string (delivered to the `run` hook) may be
a small JSON document overriding **JVM-launch** config for that one MicroVM, so a
single image can run differently-tuned instances:

```json
{ "minMemory": "6G", "maxMemory": "8G",
  "extraJvmArgs": ["-XX:+UseZGC"], "extraServerArgs": ["--world", "nether"] }
```

Only those four fields are accepted. An empty payload uses the base config; a
non-empty payload that is malformed or names any other field fails the `run` hook
(500), so the MicroVM won't serve with a misconfiguration.

The `Dockerfile` builds the image entrypoint (`hytale-runner serve`) on a JRE
over an Amazon Linux 2023 base; override `BASE_IMAGE` with your Lambda-published
`base-image-arn` when building for Lambda.

[mvm]: https://docs.aws.amazon.com/lambda/latest/dg/lambda-microvms-guide.html

## Configuration

Resolved in order: **flags > env > config file > defaults**.

- Flags: `--max-memory 8G` (see `hytale-runner --help`)
- Env: `HYRUN_` + the flag in upper snake case, e.g. `HYRUN_MAX_MEMORY`, `HYRUN_REGISTRY`
- File: `./hytale-runner.yaml` or `/etc/hytale-runner/`, or `--config <path>`

| Flag | Env | Default |
|------|-----|---------|
| `--data-dir` | `HYRUN_DATA_DIR` | `/data` |
| `--min-memory` / `--max-memory` | `HYRUN_MIN_MEMORY` / `HYRUN_MAX_MEMORY` | `6G` |
| `--assets-path` | `HYRUN_ASSETS_PATH` | `/hytale/Assets.zip` |
| `--server-jar-path` | `HYRUN_SERVER_JAR_PATH` | `/hytale/HytaleServer.jar` |
| `--registry` | `HYRUN_REGISTRY` | `localhost:5001` |
| `--state-repo` | `HYRUN_STATE_REPO` | `hytale/state` |
| `--state-tag` | `HYRUN_STATE_TAG` | `latest` |
| `--plain-http` | `HYRUN_PLAIN_HTTP` | `true` |
| `--java-bin` | `HYRUN_JAVA_BIN` | `java` |
| `--log-level` | `HYRUN_LOG_LEVEL` | `info` |
| `--extra-jvm-args` | `HYRUN_EXTRA_JVM_ARGS` | – |
| `--extra-server-args` | `HYRUN_EXTRA_SERVER_ARGS` | – |
| `--hook-port` | `HYRUN_HOOK_PORT` | `8080` |
| `--terminate-grace` | `HYRUN_TERMINATE_GRACE` | `30s` |

`--extra-jvm-args` / `--extra-server-args` are repeatable and slot in around the
jar:

```
java -Xms.. -Xmx.. <extra-jvm-args> -jar <jar> --assets <zip> <extra-server-args>
```

## Make targets

```sh
make build      # build ./hytale-runner (version from git)
make run        # run the server (ARGS="--log-level debug")
make test       # run tests
make ci         # fmt-check + vet + test
make dev-<x>    # run any target inside the dev container, e.g. make dev-test
```

Run `make help` for the full list.

## License

[MIT](./LICENSE) © 2026 Voidcube
