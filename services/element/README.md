# Element

Podman Compose stack for Element Web with a local Synapse homeserver and Postgres database.

Persistent runtime state is kept under the repository root at `./data/element`, which is already ignored by git. The compose file lives in `services/element`, so the generated env points at `../../data/element` relative to that directory:

- `./data/element/element-web/config.json`
- `./data/element/synapse`
- `./data/element/postgres`

## Start

From the repository root:

```sh
nix develop
services/element/bootstrap/init.sh
podman compose --env-file services/element/.env -f services/element/compose.yaml up -d
```

Element Web will listen on `http://localhost:8080`.
Synapse will listen on `http://localhost:8008`.

## Create A User

After the stack is running:

```sh
podman compose --env-file services/element/.env -f services/element/compose.yaml exec synapse \
  register_new_matrix_user http://localhost:8008 -c /data/homeserver.yaml
```

Use `@user:localhost` style Matrix IDs with the default local configuration.

## Bot Account

The bootstrap script writes `ELEMENT_BOT_USERNAME` and `ELEMENT_BOT_PASSWORD` to `services/element/.env` and to `~/.env` under a marked block. After the stack is up, register that account with:

```sh
services/element/bootstrap/register-bot.sh
```

## Configuration

Edit `services/element/.env` before running the bootstrap script if you need different ports, a real Matrix server name, or a public homeserver URL. If you change `ELEMENT_HOMESERVER_URL` later, rerun:

```sh
services/element/bootstrap/init.sh
podman compose --env-file services/element/.env -f services/element/compose.yaml restart element-web
```

For a public deployment, put Element Web and Synapse behind HTTPS reverse proxy endpoints and set:

- `MATRIX_SERVER_NAME` to the Matrix server name users should have in their IDs.
- `ELEMENT_HOMESERVER_URL` to the browser-accessible Synapse client URL.
