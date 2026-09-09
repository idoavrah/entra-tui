# entra-tui

[![CI](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml)

A terminal UI for **Microsoft Entra ID**, in the spirit of
[k9s](https://k9scli.io) and [terraform-tui](https://github.com/idoavrah/terraform-tui).

Browse users, groups, app registrations and enterprise applications from the
keyboard, through **your own permissions** — entra-tui signs you in and issues
nothing but `GET` requests to Microsoft Graph v1.0.

```
Tenant   aetherforge-demo-tenant                : view    esc back          [0] stormrider         [5]
Account  kaelen.stormrider0@aetherforge.onmi…   / search  c copy            [1] engine             [6]
Signed   demo                                   ~ home    ↑↓ move           [2]                    [7]
Status   connected                              ? help    ←→ tab            [3]                    [8]
                                                q quit    pg↑↓ page         [4]                    [9]
┌─────────────────────────────────── Users · 25 of 25 · search: stormrider · ↑name ────────────────────────────────────┐
│NAME                            USER PRINCIPAL NAME                         TYPE   ENABLED DEPARTMENT                 │
│Bram Stormrider                 bram.stormrider8@aetherforge.onmicrosoft.c… Member yes     Publishing                 │
│Cassia Stormrider               cassia.stormrider7@aetherforge.onmicrosoft… Guest  yes     Art & Animation            │
│Corvin Stormrider               corvin.stormrider14@aetherforge.onmicrosof… Member no      Engine                     │
│Draxin Stormrider               draxin.stormrider2@aetherforge.onmicrosoft… Member yes     Engine                     │
│Elowen Stormrider               elowen.stormrider15@aetherforge.onmicrosof… Member yes     Gameplay                   │
│Fenris Stormrider               fenris.stormrider16@aetherforge.onmicrosof… Member yes     Quality Assurance          │
│Ilyana Stormrider               ilyana.stormrider13@aetherforge.onmicrosof… Guest  no      Engine                     │
│Isolde Stormrider               isolde.stormrider17@aetherforge.onmicrosof… Guest  yes     Art & Animation            │
│Jorund Stormrider               jorund.stormrider18@aetherforge.onmicrosof… Member yes     Art & Animation            │
│Kaelen Stormrider               kaelen.stormrider0@aetherforge.onmicrosoft… Member no      Live Ops                   │
│Lyra Stormrider                 lyra.stormrider19@aetherforge.onmicrosoft.… Member yes     Live Ops                   │
│Nyx Stormrider                  nyx.stormrider6@aetherforge.onmicrosoft.com Member yes     Narrative                  │
│Osric Stormrider                osric.stormrider20@aetherforge.onmicrosoft… Member yes     Gameplay                   │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

enter describe  ·  0-9 replay a search  ·  r refresh
Dashboard › Users "stormrider"
```


## Install

```sh
go install github.com/idoavrah/entra-tui/cmd/entra-tui@latest
```

Or download a binary from the [releases](https://github.com/idoavrah/entra-tui/releases),
or build from source:

```sh
git clone https://github.com/idoavrah/entra-tui
cd entra-tui
go build ./cmd/entra-tui
```

## Try it without a tenant

```sh
entra-tui -demo
```

Demo mode runs against a generated directory served in-process — a fictional
game studio with ~420 people, 130 groups, 85 app registrations and 160
devices. No tenant, no sign-in, no network. It is what the screen captures in
[`docs/screens/`](docs/screens) are rendered from.

## How it flows

entra-tui has three levels, and **nothing is queried until you ask for it**:

1. **Login** — pick a sign-in method. No network traffic before you choose.
2. **Dashboard** — pick a view. Still no queries; the dashboard also shows
   each view's recent searches.
3. **A view** — the table. `esc` walks back a level, `~` jumps home from
   anywhere.

Sign-in happens on start, unattended, from your existing `az login` session —
there is no login screen to click past. If it fails, the dashboard says so and
`r` retries.

General keys live in the header, k9s-style; the footer carries only what the
screen in front of you does.

The dashboard is a tile grid in the spirit of k9s's pulse screen: one card
per view, each showing the directory-wide total in large numerals. Totals come
from Graph's `$count` endpoint, which returns a bare integer for the whole
collection — the only cheap way to size a directory, since paging one to count
it would cost thousands of requests. A view you cannot enumerate simply shows
no number; `r` retries.

## Signing in

entra-tui only ever acts as **you** — a delegated token, never an app-only
one. There is no client-secret mode, by design.

| Method | What happens |
| --- | --- |
| **Azure CLI** (default) | Borrows the Graph token from an existing `az login` session. No prompt at all. |
| **Browser** (`-auth browser`) | OAuth 2.0 authorization code + PKCE against a loopback redirect. MFA and Conditional Access work exactly as on the web. |

**entra-tui signs in through the Azure CLI, without asking.** An existing
`az login` session is a decision you already made, and entra-tui keeps **no
token cache on disk**, so reusing it is what saves a browser round trip on
every launch. The browser flow is still implemented and reachable with
`-auth browser`, but it is not offered in the interface.

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
| `Device.Read.All` | Devices |
| `Group.ReadWrite.All` | Groups and their owners |
| `GroupMember.ReadWrite.All` | A user's groups, a group's members |
| `Application.ReadWrite.All` | App registrations, enterprise apps, owners, role assignments |

A `ReadWrite` scope covers its `Read` counterpart, so asking for both would
only lengthen the consent prompt.

It deliberately does **not** ask for `Directory.Read.All`, which would grant
far more than these views need.

All of these require **admin consent** — that is a property of the Microsoft Graph
permission model, not of this tool: no delegated scope can enumerate a
directory without it. A `403` in the status bar means exactly this, and says
so. Individual sections degrade on their own: if you cannot read owners, the
Owners section explains that instead of the whole view failing.

entra-tui reads the directory and can **add and remove members and owners**.
It creates, deletes and renames nothing.

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
| `5` | `:devices` | Devices | `/devices` |

**Search results are sorted by name**; an unfiltered view is not. Graph
refuses `$orderby` alongside `$search`, so matches would otherwise arrive in
relevance order, which is not an order anyone can scan — a handful of them is
worth sorting client-side, and later pages of matches merge into place rather
than appending at the bottom. An unfiltered view stays in the order the
directory returned it, because sorting it would re-order the rows already on
screen every time a page lands, moving the row under the cursor while it is
being read. Your selected row follows its object across a re-sort.

Some columns are computed rather than copied straight out of Graph:

- **Groups → TYPE** collapses `groupTypes` / `mailEnabled` / `securityEnabled`
  into the label the Entra portal shows.
- **App registrations → REDIRECTS** totals the redirect URIs across the three
  platform objects Graph splits them over, and **SECRETS** counts client
  secrets and certificates together — both are ways in.
- **App registrations → CRED EXP** shows when the next secret or certificate
  lapses — or `expired`.

Rows are tinted as a whole rather than per cell: a disabled user or service
principal is dimmed, a non-compliant device or an app with a lapsed credential
is warned. Reading one flag out of a column of fifty is what a colour is for.

## Layout

The header and the hint bar are fixed height and sit outside the frame, so
content never shifts them. Everything between is boxed, with the screen's
identity and metadata centred on the top border and unfetched pages
advertised on the bottom one.

The command and search line is only drawn while `:` or `/` is open — an idle
prompt is a wasted row.

## Search

`/` searches **the whole directory**, not just the rows on screen: it
re-queries Graph with `$search`. That is the only thing that works in a tenant
of any size, where what you are looking for has usually not been paged in yet.

Every search you run is kept per view in one of ten **fixed slots**, shown as
two columns in the header and replayed with `1`–`9` and `0`.

The grid is a fixed size and sits beside the wordmark; it does not stretch
with the terminal.

Slots do not rearrange. A new term takes the next free slot and, once all ten
are used, overwrites the oldest **in place**; re-running an existing term
leaves it exactly where it is. A most-recently-used list would reshuffle the
bar on every search, so the digit that ran `finance` a moment ago would run
something else next time — which makes the shortcuts useless from memory.

The lists are per-view — the term that finds a person is rarely the term that
finds an app registration. Opening `/` starts from an empty pattern rather
than the term in force: the common case is looking for something new, and the
previous term is one keystroke away in its slot.

Slots are cached under your user cache directory (`~/.cache/entra-tui/` on
Linux, `~/Library/Caches/entra-tui/` on macOS) so they survive a restart. The
file is owner-readable only, since search terms can name people.

`esc` backs out one layer at a time. From a table it leaves for the dashboard
— it does not unpick the search on the way out, which cost a second press and
a wasted round trip; run an empty search to clear one. From a detail pane it
returns to the object you followed a link from, if there was one, and only
then to the table.

## Detail view

`enter` describes the selected object. The pane re-reads it **without** a
`$select` projection and gathers the follow-up lookups that make it
intelligible, then groups everything into sections.

`enter` in a membership list opens that object in a pane of its own, so a
group's members and a user's groups are traversable rather than dead text.
The trail along the bottom row shows how far in you are, and `esc` unwinds it
one object at a time.

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

**Users** — Essentials · Organisation · **Groups** · Contact · On-premises

*Groups* lists the groups and directory roles the user was actually added to
(direct `memberOf`, not the transitive closure, which is unhelpfully long on a
well-nested tenant).

**Groups** — Essentials · Membership · **Owners** · **Members** · Mail ·
On-premises

**Devices** — Essentials · Platform · Compliance & management · Activity ·
Registered owners · Groups

Label columns size themselves to the longest label present rather than
wrapping it, and on a wide terminal the sections spread across two columns.
Sections are never split across the boundary; the cut is chosen to minimise
the taller column.

Anything Graph returned that no section claims still appears under *Other
properties* — the grouped view never hides data the raw view would show.
`R` toggles raw JSON.

The pane is split. The object's **own fields stay at the top**; everything
that enumerates *other* objects — members, owners, groups, roles, API
permissions, credentials — becomes a **tab strip below**. An unbounded member
list has no business pushing an object's own fields off the screen.

`←`/`→` switch tabs, `↑`/`↓` walk the active list, `pgup`/`pgdn` scroll the
properties above.

### Adding and removing people

`a` adds a member, `o` adds an owner — both in two steps. You type a name,
sign-in name or email; entra-tui searches the directory and **only proceeds
when exactly one object matches**. Two matches is refused with a "be more
specific", because adding the wrong person is not a mistake worth risking on
a guess.

`d` removes the selected entry. Both confirmations name every party by
**display name and object id** — display names are not unique in a directory,
and confirming against the wrong "Ada Lovelace" is precisely what the dialog
exists to prevent.

Only `y` proceeds. Every other key, `enter` included, cancels: a confirmation
that commits on `enter` is one held-down key away from a change nobody meant
to make. After a change the object is re-read from Graph, so the pane shows
what the directory holds rather than what was asked for.

### Jumping between the two halves of an app

An Entra application is two objects: the **app registration** (`/applications`)
and the **enterprise app** (`/servicePrincipals`). Press **`x`** in a detail
view to jump to the other one. The pairing is resolved while the detail loads,
so the key hint names where it will take you, and the jump is instant.

Many enterprise apps — every Microsoft-published one — have no app
registration in your tenant. entra-tui says so rather than failing.

## Right-to-left text

Hebrew (and Arabic) display names are reordered so they read correctly in a
terminal that has no idea what bidi is — which is almost all of them. They
stay **left-aligned** like every other cell: reordering is what makes the text
readable, and a ragged left edge costs more in a dense table than
right-alignment is worth.

This is a deliberate simplification of [UAX #9](https://unicode.org/reports/tr9/):
neutral characters resolve from surrounding context, right-to-left runs are
reversed, paired brackets are mirrored, and embedded numbers keep their
reading order (`15`, not `51`). Explicit embedding controls are not
implemented. **The underlying data is never modified** — only what is drawn.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move cursor, or walk the list tab in front |
| `pgup` / `pgdn` | Page the table, or the list tab in front |
| `g` / `G` | Top / bottom |
| `enter` | Describe the selected object, in a table or in a list tab |
| `/` | Search the directory |
| `0`–`9` | Replay a search slot (in a view) |
| `1`–`5` | Open a view (on the dashboard) |
| `←` / `→` | Switch detail tabs; move between dashboard tiles |
| `:` | Command prompt (`:users`, `:groups`, `:appregs`, `:entapps`, `:dash`, `:q`) |
| `esc` | Back one layer: out of a linked object, then out of the pane, then to the dashboard |
| `~` | Dashboard, from anywhere |
| `x` | App registration ⇄ enterprise app |
| `R` | Raw JSON (detail view) |
| `r` | Refresh from Graph |
| `c` (or `y`) | Copy object id (OSC 52, works over SSH) |
| `a` | Add to the list tab in front — a member on one tab, an owner on the next |
| `d` | Remove the selected member or owner |
| `?` | Help |
| `q` | Quit |

## When Graph rejects a property

Which properties exist varies with tenant configuration and with the
collection queried — `servicePrincipal`'s `publisherName` is one that some
tenants refuse outright, failing the whole request rather than ignoring the
field.

Rather than shipping a lowest-common-denominator query, entra-tui sends the
richest one, reads the rejected property out of the error, drops it and
retries. The property is remembered, so the cost is one extra round trip per
property per session, and the column simply comes back empty instead of the
view failing.

## Paging

entra-tui requests 100 objects per page (`-page-size`) and fetches the next
page automatically as you approach the bottom, so scrolling is the whole
interface: there is no key to page, and no way to pull an entire tenant at
once. The frame reports `more below — scroll to load` until everything is in.

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
| `-view` | — | `users` (preselects a dashboard tile) |
| `-demo` | — | off — runs against a generated directory |
| `-nodelay` | — | off — opens an object's pane before it has loaded |
| `-version` | — | print the build stamp and exit |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

## Development

```sh
go test ./...        # unit tests, no network
go vet ./...
gofmt -l .
```

Layout:

```
cmd/entra-tui/       entry point: parse config, start the TUI
internal/auth/       delegated token acquisition (Azure CLI, PKCE)
internal/bidi/       right-to-left reordering for non-bidi terminals
internal/config/     flag + environment resolution
internal/demo/       generated directory and an in-process Graph stand-in
internal/graph/      paged Graph client, resources, detail sections
internal/ui/         Bubble Tea model, screens, dialogs, frame and table layout
```

### Screen captures

`docs/screens/*.txt` are rendered from the demo directory by a test, not
taken by hand. Regenerate them after a layout change:

```sh
go test ./internal/ui -run ScreenCaptures -update
```

CI fails if they are stale, so a change to the layout shows up as a readable
diff in review rather than as nothing at all.

### The demo stand-in enforces `$select`

`internal/demo` refuses a projection naming a property its collection does not
declare, exactly as Graph does. That is deliberate: a fake that quietly
ignores `$select` let a bad projection on `memberOf` — a `directoryObject`
collection asked for a group-only property — reach a real tenant, where it
failed the whole request. The stand-in catches that class of bug locally now.

### Releases

Tagging `v*` runs [GoReleaser](https://goreleaser.com), which builds Linux,
macOS and Windows binaries for amd64 and arm64, and publishes them with
checksums. The tests run first: a release that does not pass them is worse
than no release.

The Graph client keeps objects as loosely typed maps rather than generated
structs, which is why the detail pane can show every property Graph returns
without a schema having to know about it first.

## Not implemented (yet)

- Device-code sign-in for headless sessions with no browser
- A persistent token cache
- Any write beyond membership and ownership: nothing creates, deletes or
  renames a directory object

## License

MIT
