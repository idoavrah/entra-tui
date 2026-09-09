# entra-tui

A read-only terminal UI for **Microsoft Entra ID**, in the spirit of
[k9s](https://k9scli.io) and [terraform-tui](https://github.com/idoavrah/terraform-tui).

Browse users, groups, app registrations and enterprise applications from the
keyboard, through **your own permissions** — entra-tui signs you in and issues
nothing but `GET` requests to Microsoft Graph v1.0.

```
Tenant   contoso.onmicrosoft.com                                    ┌─┐┌┐┌┌┬┐┬─┐┌─┐  ┌┬┐┬ ┬┬
Account  ada@contoso.com  ·  browser                                ├┤ │││ │ ├┬┘├─┤   │ │ ││
Status   connected                                                  └─┘┘└┘ ┴ ┴└─┴ ┴   ┴ └─┘┴
: view   / search
[1] liskov  [2] finance  [3] guest
Users  [16 of 16]  ↑name  search:liskov
NAME                     USER PRINCIPAL NAME                TYPE   ENABLED JOB TITLE   DEPARTMENT
Ada Liskov               ada.liskov@contoso.com             Member yes     Engineer    Research
Barbara Liskov           barbara.liskov@contoso.com         Member yes     Director    Finance
                ןהכ הרש  sara.cohen@contoso.com             Member yes     Analyst     Security

enter describe · / search · 1-0 recent · : view · n/A more · r refresh · esc dashboard · ? help
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

## How it flows

entra-tui has three levels, and **nothing is queried until you ask for it**:

1. **Login** — pick a sign-in method. No network traffic before you choose.
2. **Dashboard** — pick a view. Still no queries; the dashboard also shows
   each view's recent searches.
3. **A view** — the table. `esc` walks back a level, `~` jumps home from
   anywhere.

## Signing in

entra-tui only ever acts as **you** — a delegated token, never an app-only
one. There is no client-secret mode, by design.

| Method | What happens |
| --- | --- |
| **Browser** | OAuth 2.0 authorization code + PKCE. A loopback listener starts, your system browser opens the real Entra sign-in page, and the code comes back on the redirect. MFA and Conditional Access work exactly as on the web. If the browser does not open, the URL is shown on screen to copy. |
| **Azure CLI** | Borrows the Graph token from an existing `az login` session. No prompt at all. |

The login screen preselects the Azure CLI when `az` is on your `PATH`, since
entra-tui keeps **no token cache on disk** and that saves a browser round trip
on every launch. `-auth browser` or `-auth azurecli` preselects a method;
you still confirm it.

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

export ENTRA_TUI_CLIENT_ID=<the appId you just created>
export ENTRA_TUI_TENANT_ID=<your tenant id or domain>
```

The app registration must be a **public client** (no secret) with
`http://localhost` as a redirect URI under *Mobile and desktop applications*.

### Permissions

entra-tui requests the least-privilege delegated scopes that cover its views:

| Scope | Covers |
| --- | --- |
| `User.Read.All` | Users |
| `Group.Read.All` | Groups |
| `Application.Read.All` | App registrations, enterprise apps, owners, role assignments |

It deliberately does **not** ask for `Directory.Read.All`, which would grant
far more than these views need.

All three require **admin consent** — that is a property of the Microsoft Graph
permission model, not of this tool: no delegated scope can enumerate a
directory without it. A `403` in the status bar means exactly this, and says
so. Individual sections degrade on their own: if you cannot read owners, the
Owners section explains that instead of the whole view failing.

Override the scopes for an even narrower token:

```sh
entra-tui -scopes User.Read.All,Group.Read.All
```

## Views

| # | Command | View | Graph collection |
| --- | --- | --- | --- |
| `1` | `:users` | Users | `/users` |
| `2` | `:groups` | Groups | `/groups` |
| `3` | `:appregs` | App registrations | `/applications` |
| `4` | `:entapps` | Enterprise apps | `/servicePrincipals` |

Every view is **sorted by name**. Sorting happens client-side because Graph
refuses `$orderby` alongside `$search`; doing it here means the order is the
same whether or not a search is active, and later pages merge into place
rather than appending at the bottom. Your selected row follows its object
across a re-sort.

Some columns are computed rather than copied straight out of Graph:

- **Groups → TYPE** collapses `groupTypes` / `mailEnabled` / `securityEnabled`
  into the label the Entra portal shows.
- **App registrations → CRED EXP** shows when the next secret or certificate
  lapses — or `expired`.

## Search

`/` searches **the whole directory**, not just the rows on screen: it
re-queries Graph with `$search`. That is the only thing that works in a tenant
of any size, where what you are looking for has usually not been paged in yet.

Every search you run is remembered per view and offered back on the number
keys:

```
[1] liskov  [2] finance  [3] guest
```

Press `1`–`9` or `0` to replay one. The lists are per-view — the term that
finds a person is rarely the term that finds an app registration — and the
dashboard shows them too, so a repeat lookup is two keys from the home screen.
History lives for the life of the process; nothing is written to disk.

`esc` clears the search; a second `esc` returns to the dashboard.

## Detail view

`enter` describes the selected object. The pane re-reads it **without** a
`$select` projection and gathers the follow-up lookups that make it
intelligible, then groups everything into sections.

**App registrations** — Essentials · Authentication · Certificates & secrets ·
API permissions · App roles · Exposed API · Owners

API permissions are resolved from GUIDs to names by looking up each referenced
API's service principal, so you see `Delegated User.Read.All` rather than
`Scope e1fe6dd8-…`. Near-expiry credentials and implicit-grant issuance are
highlighted.

**Enterprise apps** — Essentials · Properties · Users and groups · App roles ·
Exposed permissions · Owners

*Users and groups* lists the actual `appRoleAssignedTo` assignments with the
role each grants, falling back to *Default Access* the way the portal does.

**Users and groups** get Essentials / Organisation / Contact / On-premises
sections.

Anything Graph returned that no section claims still appears under *Other
properties* — the grouped view never hides data the raw view would show.
`R` toggles raw JSON.

### Jumping between the two halves of an app

An Entra application is two objects: the **app registration** (`/applications`)
and the **enterprise app** (`/servicePrincipals`). Press **`x`** in a detail
view to jump to the other one. The pairing is resolved while the detail loads,
so the key hint names where it will take you, and the jump is instant.

Many enterprise apps — every Microsoft-published one — have no app
registration in your tenant. entra-tui says so rather than failing.

## Right-to-left text

Hebrew (and Arabic) display names are reordered for display and right-aligned
within their column, so they read correctly in a terminal that has no idea
what bidi is — which is almost all of them. The column does not move; only the
text inside it.

This is a deliberate simplification of [UAX #9](https://unicode.org/reports/tr9/):
neutral characters resolve from surrounding context, right-to-left runs are
reversed, paired brackets are mirrored, and embedded numbers keep their
reading order (`15`, not `51`). Explicit embedding controls are not
implemented. **The underlying data is never modified** — only what is drawn.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move cursor |
| `pgup` / `pgdn`, `g` / `G` | Page, top / bottom |
| `enter` | Select / describe |
| `/` | Search the directory |
| `1`–`9`, `0` | Replay a recent search (open a view, on the dashboard) |
| `:` | Command prompt (`:users`, `:groups`, `:appregs`, `:entapps`, `:dash`, `:q`) |
| `esc` | Back one layer: search → view → dashboard |
| `~` | Dashboard, from anywhere |
| `x` | App registration ⇄ enterprise app |
| `R` | Raw JSON (detail view) |
| `n` / `A` | Load next page / all pages |
| `r` | Refresh from Graph |
| `y` | Copy object id (OSC 52, works over SSH) |
| `?` | Help |
| `q` | Quit |

## Paging

entra-tui requests 100 objects per page (`-page-size`) and fetches the next
page automatically as you approach the bottom. The header reads
`[loaded of total]`. `A` loads everything remaining, stopping after 50 pages
so an enormous tenant cannot pin the UI — press `A` again to continue.

## Configuration

Every flag has an environment variable; flags win.

| Flag | Environment | Default |
| --- | --- | --- |
| `-auth` | `ENTRA_TUI_AUTH` | `auto` (preselects a login option) |
| `-client-id` | `ENTRA_TUI_CLIENT_ID` | Microsoft Graph Command Line Tools |
| `-tenant` | `ENTRA_TUI_TENANT_ID` | `organizations` |
| `-scopes` | `ENTRA_TUI_SCOPES` | the three read scopes above |
| `-page-size` | `ENTRA_TUI_PAGE_SIZE` | `100` |
| `-graph-url` | `ENTRA_TUI_GRAPH_URL` | `https://graph.microsoft.com/v1.0` |
| `-view` | — | `users` (preselects a dashboard row) |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

## Development

```sh
go test ./...        # unit tests, no network
go vet ./...
```

Layout:

```
cmd/entra-tui/       entry point: parse config, start the TUI
internal/auth/       delegated token acquisition (PKCE, Azure CLI)
internal/bidi/       right-to-left reordering for non-bidi terminals
internal/config/     flag + environment resolution
internal/graph/      paged read-only Graph client, resources, detail sections
internal/ui/         Bubble Tea model, screens, table layout
```

The Graph client keeps objects as loosely typed maps rather than generated
structs, which is why the detail pane can show every property Graph returns
without a schema having to know about it first.

## Not implemented (yet)

- Device-code sign-in for headless sessions with no browser
- A persistent token cache, and search history that survives a restart
- Writes of any kind — and that one is on purpose

## License

MIT
