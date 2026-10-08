# google-scripts-mcp

A stdio MCP server for taking stock of the Apps Script projects in one Google
account: which exist, which still run, what starts them, which functions fail,
what is deployed as a web app, and the code, pulled into a local directory to
fix under git.

This first phase is **read-only on Google's side**. The token it signs in with
cannot change, deploy or delete a script. Pushing fixes back and retiring
scripts come next, behind a fresh sign-in that asks for those permissions.

## Tools

| Tool | What it does |
|---|---|
| `scripts_inventory` | Every script found, newest-run first, with runs by status, type and function, trigger evidence, deployments, and the container document for bound scripts. Start here. |
| `script_get` | One script: metadata, container, files and the functions in each, the manifest, deployments. |
| `script_runs` | Individual runs, filterable by status, type and function. |
| `script_metrics` | Google's own active-user and execution counts, daily or weekly. |
| `script_pull` | Write a script's files into a local directory. Refuses to overwrite local edits. |
| `script_remember` / `script_forget` | Keep a local list of script ids the inventory should include: the way in for bound scripts. |

## What the API cannot see

- **Bound scripts are not listed anywhere.** A script attached to a Sheet, Doc,
  Form or Slides file is not a Drive file. If it runs, the inventory shows it
  under `unidentified` by name; open its document, then Extensions > Apps
  Script, and pass the id from the editor URL
  (`script.google.com/home/projects/<id>/edit`) to `script_remember`.
- **Triggers are not in the API.** A run of type `TIME_DRIVEN` or `TRIGGER` is
  the evidence that one exists. Removing one is done in the editor
  (Triggers, the clock icon).
- **A run says that it failed, not why.** The error text is on the editor's
  Executions page.

## One-time setup

All in a browser signed in as **the Google account that owns the scripts**.
These steps are my understanding of the Google Cloud console as of October 2026; correct them here when they turn out to be wrong.

1. **Cloud project.** At <https://console.cloud.google.com>, create a project
   (e.g. `google-scripts-mcp`).
2. **APIs.** APIs & Services > Library: enable **Apps Script API** and
   **Google Drive API**.
3. **Consent screen.** Google Auth Platform (formerly "OAuth consent screen"):
   user type **External**, any app name, your address as support and
   developer contact. Under Audience, add that account as a test user.
   - Then **Publish app** (to "In production") without submitting for
     verification. An app left in *Testing* has its refresh tokens expire
     after seven days, which means signing in again every week. Unverified and
     in production, it shows a "Google hasn't verified this app" warning at
     sign-in (Advanced > Go to … to continue) and is limited to 100 users,
     neither of which matters for one person.
4. **OAuth client.** Clients > Create client > **Desktop app**. Download the
   JSON and save it as `~/.config/google-scripts-mcp/client_secret.json`.
5. **Per-account switch.** At <https://script.google.com/home/usersettings>,
   turn **Google Apps Script API** on. This is separate from step 2, and
   without it every call fails.
6. **Sign in.**

   ```sh
   make login ACCOUNT=you@gmail.com
   ```

   Open the printed link. If the browser offers several accounts, pick the
   one you named; a sign-in as anybody else is refused and nothing is saved.
   Leave every permission ticked.
7. **Register with Claude Code.**

   ```sh
   claude mcp add -s user google-scripts -- /opt/projects/google-scripts-mcp/google-scripts-mcp
   ```

## Where pulled code goes

Not in this repository; `.gitignore` keeps it out. One private repository with a
directory per script works well:

```
/opt/projects/apps-scripts/            git init once
    intentions-import/                 script_pull into a new subdirectory
    newsletter-sender/
```

## Files it keeps

`~/.config/google-scripts-mcp/` (or `$GOOGLE_SCRIPTS_MCP_DIR`):
`client_secret.json`, `token.json` (mode 0600), `remembered-scripts.json`.
To sign out, delete `token.json` and remove the app at
<https://myaccount.google.com/connections>.
