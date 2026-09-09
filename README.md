# entra-tui

A read-only terminal UI for **Microsoft Entra ID**, in the spirit of
[k9s](https://k9scli.io) and [terraform-tui](https://github.com/idoavrah/terraform-tui).

Browse users, groups, app registrations and enterprise applications from the
keyboard, through **your own permissions** — entra-tui signs you in and issues
nothing but `GET` requests to Microsoft Graph v1.0.

```
 ENTRA-TUI                                                                                              ? help
Tenant   aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
Account  ada@contoso.onmicrosoft.com  via browser

Users  [12/50 of 3,481]  filter:security  (more — n / A)
NAME                     USER PRINCIPAL NAME                     TYPE   ENABLED JOB TITLE   DEPARTMENT   AGE
Katherine Lovelace       katherine.lovelace@contoso.com          Member yes     Engineer    Security     3y152d
Linus Hopper             linus.hopper@contoso.com                Guest  yes     Analyst     Security     3y152d
Radia Hopper             radia.hopper@contoso.com                Member yes     Analyst     Security     3y152d
Edsger Turing            edsger.turing@contoso.com               Member yes     Architect   Security     3y152d

: cmd  ·  / filter  ·  s search  ·  enter describe  ·  n/A more  ·  r refresh  ·  y yank  ·  ? help  ·  q quit
```

## Install

```sh
go install github.com/idoavrah/entra-tui/cmd/entra-tui@latest
```

Or build from source:

```sh
git clone https://github.com/idoavrah/entra-tui
cd entra-tui
go build ./cmd/entra-tui
```

## Signing in

entra-tui only ever acts as **you** — a delegated token, never an app-only
one. There is no client-secret mode, by design.

| Method | Flag | What happens |
| --- | --- | --- |
| **Browser** | `-auth browser` | OAuth 2.0 authorization code + PKCE. A local loopback listener starts, your system browser opens the real Entra sign-in page, and the code comes back on the redirect. MFA and Conditional Access work exactly as they do on the web. |
| **Azure CLI** | `-auth azurecli` | Borrows the Graph token from an existing `az login` session. No prompt at all. |
| **Auto** (default) | `-auth auto` | Tries the Azure CLI first, falls back to the browser. |

Auto mode prefers the CLI because entra-tui keeps **no token cache on disk** —
without a reusable `az` session you would get a browser popup on every launch.
The status bar always shows which method is in use, so there is no ambiguity
about whose credentials you are looking through.

Sign-in happens *before* the TUI starts, so browser prompts and progress
messages are never fighting the full-screen display.

### Which app registration?

By default entra-tui authenticates as **Microsoft Graph Command Line Tools**
(`14d82eec-204b-4c2f-b7e8-296a70dab67e`) — a Microsoft first-party public
client that already has the loopback redirect URIs an interactive flow needs.
For most tenants that means zero setup.

If your tenant blocks it, register your own and point entra-tui at it:

```sh
az ad app create --display-name entra-tui \
  --sign-in-audience AzureADMyOrg \
  --public-client-redirect-uris http://localhost
```

Then:

```sh
export ENTRA_TUI_CLIENT_ID=<the appId you just created>
export ENTRA_TUI_TENANT_ID=<your tenant id or domain>
```

The app registration must be a **public client** (no secret) with
`http://localhost` as a redirect URI under *Mobile and desktop applications*.

### Permissions

entra-tui requests the least-privilege delegated scopes that cover its four
views:

| Scope | Covers |
| --- | --- |
| `User.Read.All` | Users |
| `Group.Read.All` | Groups |
| `Application.Read.All` | App registrations **and** enterprise apps |

It deliberately does **not** ask for `Directory.Read.All`, which would grant
far more than these views need.

All three require **admin consent** — that is a property of the Microsoft Graph
permission model, not of this tool: no delegated scope can enumerate a
directory without it. In many tenants an administrator has already consented
for the Graph CLI client. If not, an admin needs to grant consent once, or you
can point entra-tui at an app registration that already has it.

A `403` in the status bar means exactly this, and says so.

Override the scopes if you want an even narrower token:

```sh
entra-tui -scopes User.Read.All,Group.Read.All
```

## Views

| # | Command | View | Graph collection |
| --- | --- | --- | --- |
| `1` | `:users` | Users | `/users` |
| `2` | `:groups` | Groups | `/groups` |
| `3` | `:apps` | App registrations | `/applications` |
| `4` | `:sp` | Enterprise apps | `/servicePrincipals` |

Some columns are computed rather than copied straight out of Graph:

- **Groups → TYPE** collapses `groupTypes` / `mailEnabled` / `securityEnabled`
  into the label the Entra portal shows (*Microsoft 365*, *Security*,
  *Mail-enabled security*, *Distribution*).
- **Groups → MEMBERSHIP** distinguishes *Dynamic* from *Assigned*.
- **App registrations → CRED EXP** shows when the next secret or certificate
  lapses — or `expired`. Expiring app credentials are a common cause of
  outages, so it gets a column rather than being buried in the detail pane.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move cursor |
| `pgup` / `pgdn` | Page |
| `g` / `G` | Top / bottom |
| `1`–`4` | Jump to a view |
| `:` | Command prompt (`:users`, `:groups`, `:apps`, `:sp`, `:q`) |
| `/` | Filter the rows already loaded |
| `s` | Server-side search (Graph `$search`) |
| `enter` | Describe the selected object |
| `R` | Toggle raw JSON in the detail pane |
| `esc` | Back; clears the filter, then the search |
| `n` | Load the next page |
| `A` | Load all remaining pages |
| `r` | Refresh from Graph |
| `y` | Copy the object id to the clipboard (OSC 52, works over SSH) |
| `?` | Help |
| `q` | Quit |

### Filtering vs. searching

They are different tools, and the distinction matters in a large tenant:

- **`/` filters what is already loaded**, instantly and locally. Terms are
  ANDed and matched across every column, so `/security architect` works
  without knowing which column holds what. Prefix a term with `!` to exclude:
  `/ada !guest`.
- **`s` searches the whole directory** by re-querying Graph with `$search`.
  Use it when what you want has not been paged in yet.

Pressing `esc` peels these back one at a time — filter first, then search.

## Paging

Graph returns directory collections a page at a time. entra-tui requests 100
objects per page (`-page-size`) and fetches the next page automatically as you
approach the bottom, so scrolling stays continuous.

The count in the resource bar reads `[loaded of total]`, or
`[matching/loaded of total]` while a filter is active. `A` loads everything
remaining, stopping after 50 pages so an enormous tenant cannot pin the UI —
press `A` again to continue.

Automatic prefetch is **suppressed while a filter is active**: a filter hides
rows, so nearing the bottom of a filtered view says nothing about how much of
the directory is loaded, and auto-fetching would quietly walk the whole tenant.
Use `n` or `A` when you want more.

## Configuration

Every flag has an environment variable; flags win.

| Flag | Environment | Default |
| --- | --- | --- |
| `-auth` | `ENTRA_TUI_AUTH` | `auto` |
| `-client-id` | `ENTRA_TUI_CLIENT_ID` | Microsoft Graph Command Line Tools |
| `-tenant` | `ENTRA_TUI_TENANT_ID` | `organizations` |
| `-scopes` | `ENTRA_TUI_SCOPES` | the three read scopes above |
| `-page-size` | `ENTRA_TUI_PAGE_SIZE` | `100` |
| `-graph-url` | `ENTRA_TUI_GRAPH_URL` | `https://graph.microsoft.com/v1.0` |
| `-view` | — | `users` |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

## Development

```sh
go test ./...        # unit tests, no network
go vet ./...
```

Layout:

```
cmd/entra-tui/       entry point: parse config, sign in, start the TUI
internal/auth/       delegated token acquisition (PKCE, Azure CLI)
internal/config/     flag + environment resolution
internal/graph/      paged read-only Graph client, resource definitions
internal/ui/         Bubble Tea model, table layout, detail and help views
```

The Graph client keeps objects as loosely typed maps rather than generated
structs, which is why the detail pane can show every property Graph returns
without a schema having to know about it first.

## Not implemented (yet)

- Device-code sign-in for headless/SSH sessions with no browser
- A persistent encrypted token cache (today, a session ends with the process)
- Writes of any kind — and that one is on purpose

## License

MIT
