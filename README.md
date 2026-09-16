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
brew install idoavrah/homebrew/entra-tui
```

Or `go install github.com/idoavrah/entra-tui/cmd/entra-tui@latest`, or grab a
binary from [Releases](https://github.com/idoavrah/entra-tui/releases).

Sign in with the [Azure CLI](https://aka.ms/azure-cli) — the only supported
method — and run it:

```sh
az login
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
| `-tenant` | `ENTRA_TUI_TENANT_ID` | the Azure CLI's active tenant |
| `-page-size` | `ENTRA_TUI_PAGE_SIZE` | `100` |
| `-graph-url` | `ENTRA_TUI_GRAPH_URL` | `https://graph.microsoft.com/v1.0` |
| `-view` | — | `users` |
| `-demo` | — | off — runs against a generated directory |
| `-nodelay` | — | off — opens a pane before it has loaded |
| `-d`, `-disable-usage-tracking` | `ENTRA_TUI_DISABLE_USAGE_TRACKING` | off — tracking is on by default |
| `-bidi` | `ENTRA_TUI_BIDI` | `auto` — see [Hebrew and Arabic](#hebrew-and-arabic) |
| `-version` | — | print the build stamp and exit |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

## Hebrew and Arabic

Right-to-left names have to be reordered exactly once: by entra-tui or by the
terminal. If both do it, or neither does, they read backwards.

| `-bidi` | Who reorders |
| --- | --- |
| `auto` | the terminal, if it is one known to (iTerm2 3.6 and later, Terminal.app, Konsole, mlterm); otherwise entra-tui |
| `on` | entra-tui — for terminals that draw text in the order it arrives |
| `off` | the terminal |

A terminal's own setting can make `auto` guess wrong. Press `b` to switch
while running. The choice is remembered for that terminal — iTerm2 and VS
Code's terminal each keep their own — and used under `auto` from then on;
`-bidi on` or `off` still wins. Pressing `b` back to what `auto` would pick
forgets it. The help screen (`?`) says which way it is set and why.

Choices are kept in `settings.json` under the user config directory
(`~/Library/Application Support/entra-tui` on macOS, `~/.config/entra-tui` on
Linux, `%AppData%\entra-tui` on Windows).

## Usage tracking

entra-tui uses [PostHog](https://posthog.com) to understand how it is used,
the same way [terraform-tui](https://github.com/idoavrah/terraform-tui) does.
It is **opt-out**: `-d`, or `ENTRA_TUI_DISABLE_USAGE_TRACKING`.

Events record the *shape* of what was done — which view was opened, that a
search ran, which relationship was edited — and never what it was done to. No
display name, object id, tenant, sign-in name or search term is ever sent.
Returning users are a two-word handle derived from a one-way hash of the
machine name; crash reports have directory names stripped first. Tracking runs
on its own goroutine, so it cannot block a keystroke or take the app down, and
demo mode disables it outright, because "no network" should mean it.

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
