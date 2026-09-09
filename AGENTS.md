# Working on entra-tui

Notes for whoever — human or agent — picks this up next. The README says what
the tool is; this says how it is built and which decisions are load-bearing.

## Shape

```
cmd/entra-tui/       parse config, start the TUI
internal/auth/       delegated token acquisition (Azure CLI, PKCE)
internal/bidi/       right-to-left reordering for non-bidi terminals
internal/config/     flag + environment resolution
internal/demo/       generated directory and an in-process Graph stand-in
internal/graph/      paged Graph client, resources, detail sections
internal/text/       what every string from outside passes through
internal/ui/         Bubble Tea model, screens, dialogs, frame and table layout
```

Dependencies flow one way: `ui` → `graph` → `text`, and `auth` → `text`.
Nothing imports `ui`. `graph` must not import `auth` (that cycle is why
`Sanitize` lives in `text` rather than in `graph`).

## Invariants

These are the things that have already broken once. Each has a test.

**Never truncate styled text by rune index.** Escape sequences are bytes in
the string and zero columns on screen, so cutting a rendered line severs a
sequence and counts escapes as characters. Build lines to fit instead. This
cost the tab bar a whole tab, and later ate the object's name off the
breadcrumb.

**`Init` and `View` take the model by value.** A mutation in either is
discarded. The sign-in attempt is numbered in `New` for exactly this reason —
incrementing it in `Init` left the login screen spinning forever.

**Heterogeneous collections take no `$select`.** `memberOf`, `members`,
`owners` and `registeredOwners` are typed as `directoryObject`, and Graph
rejects the whole request for a property that type does not declare. The
default projection carries `displayName` and the `@odata.type` annotation,
which is all they need — and that annotation is the *only* thing telling a
nested group from a user in one.

**Everything from outside goes through `text.Sanitize`.** A display name is
attacker-controlled in any real tenant. `Item.String`, `Item.Strings`, Graph's
error text and the unverified token claims all route through it. If you add a
new way for external text to reach the screen, route it too — and add a case
to `internal/ui/hostile_test.go`, which renders every screen from a name
carrying an OSC 52 write, a screen clear, a cursor jump and an RTL override.

**Truncate by display width, not rune count.** One CJK ideograph is two
columns. `text.Truncate` measures the way the padding measures, so the two
cannot disagree.

**Writes are never retried.** A `POST` that may already have landed is
reported, not repeated. Reads back off on 429 and 5xx; nothing retries a 4xx
except the property self-healing below.

## Layout model

The header and the footer are fixed height and sit outside the frame, so
content can never shift them. Everything between is boxed.

The header lays three blocks side by side: session context, the general key
legend, the quick-search grid — then the wordmark in whatever is left. The
wordmark is decoration and yields rather than displaces: it steps from the
full ANSI Shadow mark (68 columns) to a compact one (24) to nothing as the
terminal narrows, and `headerHeight` follows the mark it drew. The
quick-search grid is counted in that fit whether or not it holds anything,
so recording a first search cannot change the header's height.

The footer is three rows: key hints against the frame's bottom edge, a rule,
then the trail sharing its row with the status (error or flash), pushed right.

Two legends, two jobs: everything that changes screen or moves around is in
the header; the footer carries only what the screen can *do*. Don't repeat one
in the other.

## Detail panes

The object's own fields stay on top; everything enumerating other objects
becomes a tab strip below. `propertyHeight` gives the properties what they
need down to a floor, so a two-field object does not reserve half the pane and
a thirty-field one does not squeeze the lists out.

`tabRowsHeight` must subtract the table's own furniture (border, column
titles, header rule) before handing out rows — leaving it in let the selection
walk four rows past the last row drawn.

The raw view has no active tab at all (`activeSection` returns false when
`detailRaw`). That one fact routes the arrows, page keys and ends to the
document, stops the footer offering keys that act on an invisible list, and
makes `esc` back out one layer.

Panes wait for their object by default (`pending`), so a pane is drawn once
rather than filling in under the reader. `-nodelay` opens on the row's own
columns instead. Every open goes through this: from a table, from a link, and
from the `x` jump.

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

`a` and `d` act on the tab in front, which also settles which collection is
written — a device's owners live under `registeredOwners`, and the section
carries that name. Confirmations name both parties by display name *and*
object id, and only `y` proceeds.

## Search and slots

`/` re-queries Graph; there is no local filter. A search earns a slot by
finding something — recording it when typed filled the slots with
misspellings. Slots are numbered from zero so the digit on a slot is the digit
you press, never rearrange, and are cached per view under the user cache
directory at mode `0600`.

Search results are sorted by name client-side, because Graph refuses
`$orderby` alongside `$search`. Unfiltered views are **not** sorted: sorting
them re-ordered rows already on screen every time a page landed, moving the
row under the cursor as it was read.

## Right-to-left text

Most terminals do not implement the bidirectional algorithm, so a Hebrew or
Arabic name arrives in logical order and reads backwards. `internal/bidi` is a
simplified UAX #9 pass: neutrals resolve from context, right-to-left runs
reverse, paired brackets mirror, embedded numbers keep their reading order
(`15`, not `51`). Explicit embedding controls are not implemented — and
`text.Sanitize` drops them, so nothing arrives expecting them.

Cells stay left-aligned; reordering is what makes the text readable, and
right-alignment only cost a ragged left edge in a dense table. **The
underlying data is never modified**, only what is drawn.

## When Graph rejects a property

Which properties exist varies with tenant configuration, and Graph fails the
whole request rather than ignore a field it does not know:

```
Request_UnsupportedQuery: Property 'publisherName' does not exist
```

Rather than shipping a lowest common denominator, the client sends the richest
query, reads the rejected property out of the error, drops it from `$select`
or `$search` and retries — remembering it, so it costs one round trip per
property per session and the column comes back empty instead of the view
failing. `$search` fields are data in `Query` for this reason: a pre-composed
expression cannot have one field pruned out of it.

## Permissions

| Scope | Covers |
| --- | --- |
| `User.Read.All` | Users |
| `Device.Read.All` | Devices |
| `Group.ReadWrite.All` | Groups and their owners |
| `GroupMember.ReadWrite.All` | A user's groups, a group's members |
| `Application.ReadWrite.All` | App registrations, enterprise apps, owners, role assignments |

A `ReadWrite` scope covers its `Read` counterpart, so asking for both only
lengthens the consent prompt. `Directory.Read.All` is deliberately not
requested. All of these need admin consent — a property of the Graph
permission model, not of this tool. Sections degrade individually: a section
that cannot be read says so instead of failing the view.

Sign-in is delegated only. The default client is Microsoft Graph Command Line
Tools (`14d82eec-204b-4c2f-b7e8-296a70dab67e`), a first-party public client
that already has the loopback redirect URIs an interactive flow needs;
override it with `-client-id` for tenants that block it. No token cache is
written to disk.

## Testing

```sh
go test ./...
go vet ./...
gofmt -l .
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Everything runs offline. CI runs all four plus a five-platform cross-compile.

**Golden screens.** `docs/screens/*.txt` are rendered from the demo directory
by a test and committed, so a layout change shows up as a readable diff rather
than as nothing at all. CI fails if they are stale:

```sh
go test ./internal/ui -run ScreenCaptures -update
```

Time-dependent text is blanked to a fixed width, since it would otherwise
churn the diff daily without ever saying anything.

**Drive the real binary.** Unit tests miss things that only appear in a
terminal. The reliable method is a PTY plus a terminal emulator (`pyte`):
fork, set the window size, answer the OSC 11 background query — Bubble Tea
blocks on it, and the reply is what makes lipgloss choose the dark palette —
and answer DSR 6 too. Send `ESC` and `[B` in one write or they read as two
keys. This has caught four bugs in a round that the tests did not.

The PNGs in `docs/images/` come from the same rig, drawn cell by cell out of
the terminal buffer. A browser screenshot is not equivalent, and headless
Chromium renders no text in some sandboxes.

**The demo stand-in enforces `$select`.** `internal/demo` refuses a projection
naming a property its collection does not declare, exactly as Graph does, and
annotates heterogeneous collections with `@odata.type`. Both are deliberate: a
fake that quietly ignored either let real bugs through to a tenant. Keep it
strict when you extend it.

## Releases

Tagging `v*` runs GoReleaser: Linux, macOS and Windows binaries for amd64 and
arm64, published with checksums. Tests run before anything is published.
GitHub Actions are pinned to commit SHAs, not moving tags, because the release
workflow publishes under this repository's name. The `govulncheck` step sets
`GOTOOLCHAIN=auto` for itself — `setup-go` pins it to `local`, and the tool's
own `go` directive runs ahead of this module's.

## Conventions

Comments explain *why*, not what. Prefer a sentence naming the thing that went
wrong over a restatement of the code. Match the surrounding density.

Commit messages are prose, not bullet lists: what changed, and what it fixes
or prevents. Reference the failure, not the ticket.
