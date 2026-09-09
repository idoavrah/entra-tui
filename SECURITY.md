# Security

## Reporting a vulnerability

Report privately through [GitHub's advisory
form](https://github.com/idoavrah/entra-tui/security/advisories/new) rather
than a public issue. A first response usually takes a few days.

Please include what you did, what happened, and what you expected — a
reproduction against `entra-tui -demo` is ideal, since that needs no tenant
and no credentials of yours. **Do not send tokens, object ids from a real
directory, or anything else identifying real people**; the demo directory has
equivalents for all of it.

## What entra-tui is and is not trusted with

entra-tui acts as **you**, through a delegated token, and never as an
application. There is no client-secret mode and no app-only mode.

**Tokens.** No token cache is written to disk. In the default Azure CLI mode
the token is read from `az account get-access-token` for the life of the
process; in `-auth browser` mode it lives in memory only, so quitting throws
it away and the next launch signs in again. The token is never logged, never
written to a file, and never sent anywhere but `graph.microsoft.com` (or the
sovereign-cloud endpoint given by `-graph-url`).

**Access.** Everything entra-tui can see or change, you could already see or
change. It requests the narrowest delegated scopes its views need, and never
`Directory.Read.All` — see [Permissions](README.md#permissions). Consent is
the tenant's decision, not the tool's: without it, requests come back `403`
and the affected section says so.

**Writes.** entra-tui adds and removes group members and owners. It creates,
deletes and renames nothing. Every change is confirmed first, naming both
parties by display name *and* object id, because display names are not unique
in a directory. Only `y` proceeds; every other key, `enter` included, cancels.
A write is never retried, since a `POST` that may already have been applied is
reported rather than repeated.

**On disk.** One file: the quick-search slots, under your user cache
directory (`~/.cache/entra-tui/searches.json` on Linux,
`~/Library/Caches/entra-tui/` on macOS). It holds search terms, which can name
people, so it is written atomically at mode `0600`. Delete it at any time;
nothing else is kept.

**The terminal.** Directory data is not under your control — a display name is
whatever somebody typed into it, and anyone who can create an object in the
tenant can choose one. Every string from Graph is stripped of the characters a
terminal acts on rather than draws (C0 and C1 controls, `DEL`, and the
bidirectional overrides and isolates) before it is rendered, so a name cannot
move the cursor, repaint the screen, forge a confirmation dialog, or write to
your clipboard. Text is then cut by display width rather than by character
count, so a name of wide glyphs cannot overflow its column and push the frame
apart. The raw view (`R`) stays faithful — it escapes those characters as
`\uXXXX` instead of dropping them, so the true value is always one keystroke
away. The one clipboard write entra-tui makes (`c`) is an OSC 52 sequence
carrying base64 of a value the app itself produced.

## Supported versions

Only the latest release. entra-tui is pre-1.0; fixes go into the next tag
rather than into patches of older ones.
