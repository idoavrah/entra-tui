# Working on entra-tui

Notes for whoever — human or agent — picks this up next. The README says what
the tool is; this says how it is built and which decisions are load-bearing.

## Shape

```
cmd/entra-tui/       parse config, start the TUI
internal/auth/       borrows the Graph token from an `az login` session
internal/bidi/       right-to-left reordering, and whether this terminal needs it
internal/config/     flag + environment resolution
internal/demo/       generated directory and an in-process Graph stand-in
internal/graph/      paged Graph client, resources, detail sections
internal/telemetry/  anonymous opt-out usage tracking, and the update check
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

**The Azure CLI is the only way in.** `auth.Resolve` shells out to `az account
get-access-token` and nothing else. There is no client id, no scope list, no
interactive flow, no loopback listener and no fallback: the CLI already does
device codes, MFA, Conditional Access, WAM and every broker quirk on every
platform, and doing it a second time badly helps nobody. Every failure comes
back as an `auth.SignInError` carrying the command that fixes it, because
nearly all of them are a machine that needs `az login`.

**Sign-in happens before the interface opens.** `main` resolves a provider and
builds the Graph client; the model is handed one and has no sign-in state at
all. A dashboard that has drawn itself, reported "connected" and then admits
in a corner that it never signed in is worse than no dashboard. Failing out
before `tea.NewProgram` also puts the remedy on a plain terminal rather than
inside the alternate screen buffer.

**Telemetry properties describe the shape, never the subject.** Which view,
which relationship, how many — never a display name, object id, tenant, UPN or
search term. This tool browses a directory; none of it belongs in analytics.
`telemetry.Properties` carries the rule in its doc comment. Demo mode disables
tracking outright, because the README promises it makes no network calls.

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

The dashboard picks its column count from what a tile has to say, not from the
width alone: `tilePreferredWidth` measures the longest view description, and
the grid takes the most columns where every tile still clears it. Choosing on
width alone put three tiles across a 150-column terminal, four cells too
narrow for the longest one, which lost its last words to an ellipsis on every
launch. Below `twoColumnDashboard` the choice stops being wide against wider
and becomes cramped against unreadable, so a narrow terminal keeps one tile
per row.

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
| `b` | Right-to-left text: reordered by entra-tui ⇄ left to the terminal |
| `?` | Help |
| `q` | Quit |

`a` and `d` act on the tab in front, which also settles which collection is
written — a device's owners live under `registeredOwners`, and the section
carries that name. The path comes from `detailRes`, the pane's own resource,
never from `coll.res`: following a link out of a user's Groups tab leaves the
browse collection on users while the pane shows a group, and a write belongs
to what is in front of the reader. Confirmations name both parties by display name *and*
object id, and only `y` proceeds.

An enterprise application's "Users and groups" tab is writable too, but it is
not a `$ref` collection like the others: `appRoleAssignedTo` holds
`appRoleAssignment` entities, so an add POSTs all three parties (principal,
resource, role) and a delete names the *assignment's* own id — one user can
hold several roles on one application, so the principal's id would not say
which. That id rides on `Field.RefID`. An application publishing assignable
roles gets a role step with Default Access first and preselected; one
publishing none skips the step, since a list of one is a question with a
single answer. The confirmation says "Delete assignment" rather than "Delete
user or group", which would read as deleting the person from the directory.

An `a` that matches more than one object opens a picker rather than resolving
the ambiguity or refusing it. Guessing risks adding the wrong person, and
demanding a narrower term asks the user to solve a problem they cannot see —
two people really can share a display name. Picking is not writing: the
confirmation still follows, and still names the id. Rows carry the kind
because a member may be a user or a group, taken from `ObjectViewKind` so a
row cannot be labelled one thing here and another in the table it came from —
which is why the group search selects `securityEnabled`, for its presence
rather than its value.

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

Some terminals *do* reorder — iTerm2 from 3.6, Terminal.app, Konsole, mlterm —
and reordering for one of those reverses the text a second time, so every
Hebrew word reads backwards. Reordering is therefore a switch
(`bidi.SetReordering`), process-wide because it describes the one terminal
everything is drawn on. `-bidi` / `ENTRA_TUI_BIDI` sets it: `on`, `off`, or
`auto`, which goes by `TERM_PROGRAM`, `LC_TERMINAL` (iTerm2 sets it, and it
survives ssh and tmux), `KONSOLE_VERSION` and `MLTERM`. It cannot do better
than a name: a cursor position report is in logical columns either way, so no
probe tells a reordering terminal from one that does not. A name is wrong for
anybody who changed the terminal's own setting, which is why `b` switches it
at runtime and the help screen gives the launch-time reason.

`b` also remembers the choice, in `settings.json` under the user *config*
directory (`config.Settings`) — not the cache, which the system may clear.
It is keyed by `bidi.TerminalName`, without the version, because one person
can need opposite answers in iTerm2 and VS Code's terminal, and an upgrade
must not forget it. Precedence: `-bidi on|off` / `ENTRA_TUI_BIDI`, then the
remembered choice, then the guess. **A guess is never written down:** a choice
that agrees with it removes the entry instead, so a release that fixes a wrong
guess still reaches everyone who never overrode it, and pressing `b` back is
how a remembered choice is undone. `ui` never touches the file; `main` hands
it `Options.RememberBidi`, which tests leave nil.

Terminals implementing the "BiDi in Terminal Emulators" recommendation (VTE)
are told the choice with ECMA-48's BDSM (`CSI 8 l` when entra-tui reorders,
`CSI 8 h` when it does not), so on those it is exact. The sequence is written
to the terminal by a command, at start and on every switch, never inside a
frame — a frame would carry it into every golden screen. `main` writes
`CSI 8 h`, the terminal's default, on the way out. Other terminals ignore a
mode they do not know.

Every string that can hold a name goes through `bidi.Display` where it is
drawn, not where it is built, or switching would leave it behind. That includes
the flash line and the role picker's prompt, both of which once missed it.

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

| Permission | Covers |
| --- | --- |
| `User.Read.All` | Users |
| `Device.Read.All` | Devices |
| `Group.ReadWrite.All` | Groups and their owners |
| `GroupMember.ReadWrite.All` | A user's groups, a group's members |
| `Application.ReadWrite.All` | App registrations, enterprise apps, owners, role assignments |

These are what the views need, not what entra-tui requests — see below. All of
them need admin consent, a property of the Graph permission model rather than
of this tool. Sections degrade individually: one that cannot be read says so
instead of failing the view.

Sign-in is delegated only, and the permissions above are the Azure CLI's own
rather than anything entra-tui asks for: the token is minted for the CLI's
first-party client with whatever that client has been consented in the tenant.
Nothing here can widen it. No token cache is written to disk.

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
