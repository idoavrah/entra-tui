# entra-tui

[![CI](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/idoavrah/entra-tui/actions/workflows/ci.yml)

A terminal UI for **Microsoft Entra ID**, in the spirit of
[k9s](https://k9scli.io) and [terraform-tui](https://github.com/idoavrah/terraform-tui).

Browse users, groups, app registrations, enterprise apps and devices from the
keyboard, through **your own permissions**.

![The users view](docs/images/users.png)

## Install

```sh
go install github.com/idoavrah/entra-tui/cmd/entra-tui@latest
```

Or grab a binary from [Releases](https://github.com/idoavrah/entra-tui/releases).
Then run it — it borrows your existing `az login` session:

```sh
entra-tui
```

## Try it without a tenant

```sh
entra-tui -demo
```

Demo mode generates a fictional game studio's directory in process and serves
it from a Graph stand-in with **no tenant, no sign-in and no network**. Every
screenshot here is that directory — 420 people, 130 groups, 85 app
registrations, 160 devices — seeded, so it looks the same tomorrow, and salted
with disabled accounts, guests, lapsed credentials, nested groups and
right-to-left names.

## The dashboard

![The dashboard](docs/images/dashboard.png)

Nothing is queried until you ask. Sign-in runs unattended on start — no login
screen to click past — and the dashboard reports it, with `r` to retry.

The tiles are k9s's pulse screen: one card per view with the directory-wide
total from Graph's `$count` endpoint, which returns a bare integer for the
whole collection. It is the only cheap way to size a directory; paging one to
count it would cost thousands of requests. A view you cannot enumerate shows
no number.

General keys live in the header; the footer carries only what the screen in
front of you does. `esc` walks back a level, `~` jumps home.

## Views

| # | Command | View | Graph collection |
| --- | --- | --- | --- |
| `1` | `:users` | Users | `/users` |
| `2` | `:groups` | Groups | `/groups` |
| `3` | `:appregs` | App registrations | `/applications` |
| `4` | `:entapps` | Enterprise apps | `/servicePrincipals` |
| `5` | `:devices` | Devices | `/devices` |

`:` narrows as you type: `:d` leaves `dash` and `devices`, `↑`/`↓` pick.

Some columns are computed rather than copied. **Groups → TYPE** collapses
`groupTypes` / `mailEnabled` / `securityEnabled` into the label the portal
shows; **App registrations → REDIRECTS** totals the URIs across the three
platform objects Graph splits them over, **SECRETS** counts secrets and
certificates together since both are ways in, and **CRED EXP** shows when the
next lapses.

Rows are tinted whole rather than per cell — a disabled user dimmed, a lapsed
credential warned. Reading one flag out of a column of fifty is what a colour
is for, and the tint survives the cursor landing on the row.

## Search

![A search, with the slots filled](docs/images/search.png)

`/` searches **the whole directory**, not the rows on screen: it re-queries
Graph with `$search`. In a tenant of any size, what you want has usually not
been paged in yet.

A search that **finds something** takes one of ten fixed slots, replayed with
`0`–`9`. The digit on a slot is the digit you press, and a search that found
nothing is forgotten rather than sitting there all session.

Slots never rearrange: a new term takes the next free one and, once full,
overwrites the oldest **in place**. A most-recently-used list reshuffles on
every search, which makes the shortcuts useless from memory. They are per
view, cached at mode `0600` since search terms name people.

**Search results are sorted by name; an unfiltered view is not.** Graph
refuses `$orderby` alongside `$search`, so matches arrive in relevance order,
which nobody can scan — a handful is worth sorting client-side. An unfiltered
view keeps the directory's order, because sorting it would re-order rows
already on screen every time a page lands, moving the row under the cursor as
it is read.

## Detail view

![An app registration](docs/images/app-registration.png)

`enter` describes the selected object: the pane re-reads it **without** a
`$select` projection, gathers the follow-up lookups that make it intelligible,
and groups everything into sections.

It is split: the object's **own fields stay at the top**, and everything
enumerating *other* objects becomes a **tab strip below** — an unbounded
member list has no business pushing an object's own fields off the screen.
Wide terminals get two columns, cut where the taller one is shortest and never
through a section.

Anything no section claims appears under *Other properties*; the grouped view
never hides what `R`, the raw JSON, would show.

| Kind | Sections |
| --- | --- |
| App registrations | Essentials · Authentication · Certificates & secrets · API permissions · App roles · Exposed API · Owners |
| Enterprise apps | Essentials · Properties · Users and groups · App roles · Exposed permissions · Owners |
| Users | Essentials · Organisation · **Groups** · Contact · On-premises |
| Groups | Essentials · Membership · **Owners** · **Members** · Mail · On-premises |
| Devices | Essentials · Platform · Compliance & management · Activity · Registered owners · Groups |

*Users → Groups* is the direct `memberOf`, not the transitive closure, which
is unhelpfully long on a well-nested tenant. *Enterprise apps → Users and
groups* lists real `appRoleAssignedTo` assignments with the role each grants.

### API permissions

![API permissions, resolved](docs/images/api-permissions.png)

GUIDs are resolved by looking up each referenced API's service principal, so
you read `User.Read.All` rather than `Scope e1fe6dd8-…`. Delegated and
application permissions differ in consent and in blast radius, so the type
gets a column, and consent status comes from the app's own service principal —
`-` rather than a guess when it cannot be read.

Near-expiry credentials and implicit-grant issuance are flagged amber, with
the reason in the value: the implicit flow puts tokens in the URL fragment and
cannot be bound with PKCE.

### Following a link

![A member opened in its own pane](docs/images/linked-object.png)

`enter` on a row in a membership list opens that object in a pane of its own,
so a group's members and a user's groups are traversable rather than dead
text. The trail along the bottom shows how far in you are, and `esc` unwinds
it one object at a time.

Which object a row stands for comes from Graph's `@odata.type` — the only
signal there is, since a members collection is declared as `directoryObject`
and the properties that would give a row away are not even selectable.

### Adding and deleting

![A confirmation](docs/images/confirm.png)

`a` adds to the list in front — a member on one tab, an owner on the next — in
two steps: you type a name, sign-in name or email, and it **only proceeds when
exactly one object matches**. Two is refused with "be more specific". `d`
deletes the selected row from that same list.

Both confirmations name every party by **display name and object id**: display
names are not unique in a directory, and confirming against the wrong "Ada
Lovelace" is precisely what the dialog prevents. Only `y` proceeds — every
other key, `enter` included, cancels, since a dialog that commits on `enter`
is one held-down key from a change nobody meant. Afterwards the object is
re-read, so the pane shows what the directory holds.

entra-tui creates, deletes and renames nothing else.

### Jumping between the two halves of an app

An Entra application is two objects: the **app registration**
(`/applications`) and the **enterprise app** (`/servicePrincipals`). `x` jumps
between them, and the pairing is resolved while the detail loads, so the key
hint names where it will take you. Many enterprise apps — every
Microsoft-published one — have no app registration in your tenant; entra-tui
says so rather than failing.

## Right-to-left text

![Hebrew names in the table](docs/images/right-to-left.png)

Most terminals do not implement the bidirectional algorithm, so a Hebrew or
Arabic name arrives in logical order and reads backwards. `internal/bidi` is a
simplified UAX #9 pass: neutrals resolve from context, right-to-left runs
reverse, paired brackets mirror, and embedded numbers keep their reading order
(`15`, not `51`).

Cells stay **left-aligned** like any other — reordering is what makes the text
readable, and right-alignment only cost a ragged left edge in a dense table.
**The underlying data is never modified**, only what is drawn. Search terms
get the same treatment, in the prompt and in the slots.

## Untrusted text

A display name is whatever somebody typed into it, and in most tenants any
member can create an object and name it. entra-tui treats every string from
Graph as untrusted:

- Characters a terminal **acts on** rather than draws are removed — C0 and C1
  controls, `DEL`, and the bidirectional overrides. Without this a name could
  clear the screen, repaint a confirmation dialog into a decoy, or emit an
  OSC 52 sequence to write your clipboard.
- Text is cut by **display width**, not character count, so double-width
  glyphs cannot overflow a column and push the frame apart.
- The **raw view** stays faithful, escaping those characters as `\uXXXX`
  rather than dropping them, so nothing is hidden from you.

See [SECURITY.md](SECURITY.md) for the rest of the security model.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move cursor, or walk the list tab in front |
| `pgup` / `pgdn`, `g` / `G` | Page, top / bottom of the list in front |
| `enter` | Describe the selected object, in a table or a list tab |
| `/` | Search the directory |
| `0`–`9` | Replay a search slot (in a view) |
| `1`–`5` | Open a view (on the dashboard) |
| `←` / `→` | Switch detail tabs; move between dashboard tiles |
| `:` | Command prompt — type a prefix, `↑`/`↓` pick from what matches |
| `esc` | Back one layer: linked object, then pane, then home |
| `~` | Dashboard, from anywhere |
| `x` | App registration ⇄ enterprise app |
| `R` | Raw JSON (detail view) |
| `r` | Refresh from Graph |
| `c` (or `y`) | Copy object id (OSC 52, works over SSH) |
| `a` | Add to the list tab in front |
| `d` | Delete the selected row from it |
| `?` | Help |
| `q` | Quit |

## Signing in

entra-tui only ever acts as **you** — a delegated token, never an app-only
one. No client-secret mode, by design, and **no token cache on disk**.

| Method | What happens |
| --- | --- |
| **Azure CLI** (default) | Borrows the Graph token from an existing `az login` session. No prompt. |
| **Browser** (`-auth browser`) | OAuth 2.0 authorization code + PKCE, loopback redirect. MFA and Conditional Access behave as on the web. |

### Which app registration?

By default entra-tui authenticates as **Microsoft Graph Command Line Tools**
(`14d82eec-204b-4c2f-b7e8-296a70dab67e`), a first-party public client that
already has the loopback redirect URIs an interactive flow needs — zero setup
for most tenants. If yours blocks it, register your own **public client** (no
secret) with `http://localhost` under *Mobile and desktop applications*:

```sh
az ad app create --display-name entra-tui \
  --sign-in-audience AzureADMyOrg \
  --public-client-redirect-uris http://localhost

export ENTRA_TUI_CLIENT_ID=<the appId you just created>
export ENTRA_TUI_TENANT_ID=<your tenant id or domain>
```

### Permissions

| Scope | Covers |
| --- | --- |
| `User.Read.All` | Users |
| `Device.Read.All` | Devices |
| `Group.ReadWrite.All` | Groups and their owners |
| `GroupMember.ReadWrite.All` | A user's groups, a group's members |
| `Application.ReadWrite.All` | App registrations, enterprise apps, owners, role assignments |

A `ReadWrite` scope covers its `Read` counterpart, so asking for both would
only lengthen the consent prompt. entra-tui deliberately does **not** ask for
`Directory.Read.All`.

All of these need **admin consent** — a property of the Graph permission
model, not of this tool: no delegated scope enumerates a directory without it.
A `403` in the status bar means exactly that. Sections degrade individually —
if you cannot read owners, the Owners section says so instead of the view
failing.

Narrow the token further if you like:

```sh
entra-tui -scopes User.Read.All,Group.Read.All
```

## Configuration

| Flag | Environment | Default |
| --- | --- | --- |
| `-auth` | `ENTRA_TUI_AUTH` | `auto` |
| `-client-id` | `ENTRA_TUI_CLIENT_ID` | Microsoft Graph Command Line Tools |
| `-tenant` | `ENTRA_TUI_TENANT_ID` | `organizations` |
| `-scopes` | `ENTRA_TUI_SCOPES` | the scopes above |
| `-page-size` | `ENTRA_TUI_PAGE_SIZE` | `100` |
| `-graph-url` | `ENTRA_TUI_GRAPH_URL` | `https://graph.microsoft.com/v1.0` |
| `-view` | — | `users` |
| `-demo` | — | off — runs against a generated directory |
| `-nodelay` | — | off — opens a pane before it has loaded |
| `-version` | — | print the build stamp and exit |

`-graph-url` exists for sovereign clouds (US Gov, China, …).

A pane waits for its object by default, so it is drawn once rather than
opening on the row's few columns and replacing every value a moment later.

## Paging

Pages load as you scroll: reaching the last loaded row fetches the next. There
is no key that pulls a whole tenant, deliberately — the frame's bottom edge
says when more is waiting.

## When Graph rejects a property

Which properties exist varies with tenant configuration, and Graph fails the *whole
request* rather than ignore a field it does not know:

```
Request_UnsupportedQuery: Property 'publisherName' does not exist
```

Rather than shipping a lowest common denominator, entra-tui sends the richest
query, reads the rejected property out of the error, drops it and retries —
remembering it, so it costs one round trip per property per session and the
column comes back empty instead of the view failing.

## Development

```sh
go test ./...        # unit tests, no network
go vet ./...
gofmt -l .
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

CI runs all four on every push, plus a five-platform cross-compile.

```
cmd/entra-tui/       entry point: parse config, start the TUI
internal/auth/       delegated token acquisition (Azure CLI, PKCE)
internal/bidi/       right-to-left reordering for non-bidi terminals
internal/config/     flag + environment resolution
internal/demo/       generated directory and an in-process Graph stand-in
internal/graph/      paged Graph client, resources, detail sections
internal/text/       what every string from outside passes through
internal/ui/         Bubble Tea model, screens, dialogs, frame and table layout
```

### Screen captures

`docs/screens/*.txt` are rendered from the demo directory by a test, and CI
fails if they are stale, so a layout change shows up as a readable diff:

```sh
go test ./internal/ui -run ScreenCaptures -update
```

The PNGs above are drawn from the same directory, through a pseudo-terminal.

### The demo stand-in enforces `$select`

`internal/demo` refuses a projection naming a property its collection does not
declare, exactly as Graph does. That is deliberate: a fake that quietly
ignored `$select` let a bad projection on `memberOf` reach a real tenant,
where it failed the whole request.

### Releases

Tagging `v*` runs [GoReleaser](https://goreleaser.com): Linux, macOS and
Windows binaries for amd64 and arm64, published with checksums. Tests run
first.

## Not implemented (yet)

Directory roles and administrative units, conditional access policies, sign-in
and audit logs, creating or deleting objects.

## License

MIT — see [LICENSE](LICENSE).
