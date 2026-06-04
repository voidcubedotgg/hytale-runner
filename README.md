# hytale-server-runner

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
hytale-runner run            # pull state -> run server -> push state
hytale-runner state pull     # pull state into the data dir
hytale-runner state push     # push the data dir as state
hytale-runner version
```

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

`--extra-jvm-args` / `--extra-server-args` are repeatable and slot in around the
jar:

```
java -Xms.. -Xmx.. <extra-jvm-args> -jar <jar> --assets <zip> <extra-server-args>
```

## Make targets

```sh
make build      # build ./hytale-server-runner (version from git)
make run        # run the server (ARGS="--log-level debug")
make test       # run tests
make ci         # fmt-check + vet + test
make dev-<x>    # run any target inside the dev container, e.g. make dev-test
```

Run `make help` for the full list.

## License

[MIT](./LICENSE) © 2026 Voidcube
