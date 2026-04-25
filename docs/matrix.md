# Matrix / Element Adapter

Run Element separately on port `8080`; `homelabd` defaults its control API to `127.0.0.1:18080`.

Expected `~/.env` values:

```sh
ELEMENT_BOT_USERNAME="@homelabd:lab"
ELEMENT_BOT_PASSWORD="..."
ELEMENT_HOMESERVER="http://lab:8008"
ELEMENT_ROOM_NAME="first"
```

Equivalent `MATRIX_*` names are also supported:

```sh
MATRIX_HOMESERVER="http://lab:8008"
MATRIX_USER="@homelabd:lab"
MATRIX_PASSWORD="..."
MATRIX_ACCESS_TOKEN="..."
MATRIX_ROOM_ID="..."
MATRIX_ROOM_ALIAS="#first:lab"
MATRIX_ROOM_NAME="first"
MATRIX_PREFIX="!agent"
```

The stdlib Matrix adapter supports unencrypted rooms only. Use a new unencrypted room named `first`; if Element has encryption enabled in another room, create a new unencrypted room for the bot or add a full E2EE-capable Matrix client library later.

Start both the Matrix adapter and HTTP control API:

```sh
go run ./cmd/homelabd -mode matrix
```

By default the bot only responds when addressed. Examples:

```text
!agent help
element-bot: tasks
element-bot new Add structured logging to backup service
```

Drive the same daemon over HTTP:

```sh
go run ./cmd/homelabctl -addr http://127.0.0.1:18080 task list
```
