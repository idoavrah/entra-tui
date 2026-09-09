# entra-tui

[![CI](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml)

A terminal UI for **Microsoft Entra ID**, in the spirit of
[k9s](https://k9scli.io) and [terraform-tui](https://github.com/idoavrah/terraform-tui).

Browse users, groups, app registrations, enterprise apps and devices from the
keyboard, through **your own permissions** — entra-tui signs you in with your
existing `az login` session and never holds a token of its own.

![Dashboard](docs/images/dashboard.png)

More screens: [docs/screens.md](docs/screens.md)

## Install

```sh
go install github.com/idoavrah/entra-tui/cmd/entra-tui@latest
```

Or grab a binary from [Releases](https://github.com/idoavrah/entra-tui/releases),
then run it:

```sh
entra-tui
```

## Try it without a tenant

```sh
entra-tui -demo
```

Demo mode generates a fictional game studio's directory in process and serves
it from a Graph stand-in with **no tenant, no sign-in and no network** — 420
people, 130 groups, 85 app registrations, 160 devices. Every screenshot in
this repository is that directory.

## Views

| # | Command | View | Graph collection |
| --- | --- | --- | --- |
| `1` | `:users` | Users | `/users` |
| `2` | `:groups` | Groups | `/groups` |
| `3` | `:appregs` | App registrations | `/applications` |
| `4` | `:entapps` | Enterprise apps | `/servicePrincipals` |
| `5` | `:devices` | Devices | `/devices` |

`/` searches the whole directory, `enter` describes an object, `a` and `d` add
to and delete from the list in front, and `?` lists every key.

entra-tui reads the directory and edits only group members and owners. It
creates, deletes and renames nothing. See [SECURITY.md](SECURITY.md) for the
permissions it requests and the rest of the security model.

## Configuration

| Flag | Environment | Default |
| --- | --- | --- |
| `-auth` | `ENTRA_TUI_AUTH` | `auto` |
| `-client-id` | `ENTRA_TUI_CLIENT_ID` | Microsoft Graph Command Line Tools |
| `-tenant` | `ENTRA_TUI_TENANT_ID` | `organizations` |
| `-scopes` | `ENTRA_TUI_SCOPES` | see [SECURITY.md](SECURITY.md) |
| `-page-size` | `ENTRA_TUI_PAGE_SIZE` | `100` |
| `-graph-url` | `ENTRA_TUI_GRAPH_URL` | `https://graph.microsoft.com/v1.0` |
| `-view` | — | `users` |
| `-demo` | — | off — runs against a generated directory |
| `-nodelay` | — | off — opens a pane before it has loaded |
| `-version` | — | print the build stamp and exit |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

## Development

```sh
go test ./...        # offline
go vet ./...
gofmt -l .
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

CI runs all four on every push, plus a five-platform cross-compile.
[AGENTS.md](AGENTS.md) has the architecture, the invariants worth knowing
before you change anything, and how the screens are captured.

## Not implemented (yet)

Directory roles and administrative units, conditional access policies, sign-in
and audit logs, creating or deleting objects.

## License

MIT — see [LICENSE](LICENSE).
