# retro

retro is a small Go CLI for keeping daily work notes in an Obsidian vault with enough structure to support weekly reviews, milestone tracking, and Copilot-assisted summaries.

## Why this shape

- Daily markdown files live inside your Obsidian vault, so Obsidian continues to handle sync and search.
- Quick capture commands keep the friction low enough to use throughout the day.
- The generated note template separates accomplishments, milestones, work in progress, important information, ideas, and general notes.
- `retro ask` enumerates recent markdown files and asks Copilot CLI to read those files directly from disk.

## Build

```powershell
go build ./...
```

## Requirements

- GitHub Copilot CLI must be installed. See the GitHub docs for installation and setup.
- Copilot CLI must trust the vault directory that retro will use.
- The current implementation is heavily geared toward Windows path handling and PowerShell-based usage. macOS support is planned but not fully validated yet.

For Copilot CLI trusted directories, GitHub documents that trusted folders are stored in the Copilot CLI `config.json` file. The relevant docs are here:

- https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/configure-copilot-cli#editing-trusted-directories

On Windows, that file is typically:

```text
%USERPROFILE%\.copilot\config.json
```

You should make sure the `trustedFolders` array includes your Obsidian vault path, for example:

```json
{
  "trustedFolders": [
    "C:\\Users\\you\\Documents\\Vault"
  ]
}
```

Without that trust configuration, Copilot CLI may refuse to read notes from the vault even if retro can locate them.

## Configure

```powershell
retro init --vault "C:\Users\you\Obsidian\WorkVault"
```

This writes config to your user config directory. Set `RETRO_CONFIG` if you want to override the location.

Default config:

```json
{
  "vault_path": "C:\\Users\\you\\Obsidian\\WorkVault",
  "notes_dir": "Worklog",
  "copilot_command": "copilot -C {vault_path} -p {prompt} --allow-all-tools -s"
}
```

`copilot_command` is intentionally generic:

- If it contains `{prompt}`, retro replaces that token with the generated prompt.
- If it contains `{prompt_file}`, retro writes the generated prompt to a temporary file and replaces that token with the file path.
- If it contains `{vault_path}`, retro replaces that token with the configured vault path.
- The prompt is also sent to stdin.
- The prompt is also available through the `RETRO_PROMPT` environment variable.
- If `{prompt_file}` is used, the file path is also available through the `RETRO_PROMPT_FILE` environment variable.
- When the configured command is `copilot`, retro also adds the vault path automatically with `-C`, `--add-dir`, and `--allow-all-paths` unless you already set them.

That gives you room to adapt to whichever Copilot CLI flow you actually use.

## Capture notes

```powershell
retro done "Finished the API pagination work"
retro wip "Still debugging the sync timeout"
retro idea "Cache the results in Redis before hitting the vendor API"
retro info "Customer asked for weekly export in CSV format"
retro milestone "Released v2.3.0 to production"
retro note "Need to follow up with ops tomorrow"
```

You can also use the generic command:

```powershell
retro capture --type accomplishment "Closed the auth migration"
```

Daily notes are written to:

```text
<vault>/<notes_dir>/<year>/<month>/<YYYY-MM-DD>.md
```

For an Obsidian vault that keeps work notes under `Worklog/<year>/<month>/`, the defaults now match that layout directly.

For `retro ask`, the prompt lists the note files for the time window instead of embedding all note contents inline. That keeps prompts smaller and lets Copilot inspect the original markdown files directly.

Template-only daily notes are skipped automatically, so blank Worklog entries do not dilute the summary.

## Review and summarize

Print today's note path:

```powershell
retro today
```

Print today's note contents:

```powershell
retro today --print
```

Build a prompt from the last 7 days of notes and print it instead of running Copilot:

```powershell
retro ask --days 7 --print-only "Summarize my biggest achievements and open threads"
```

Run your configured Copilot command:

```powershell
retro ask --days 7 "Summarize my biggest achievements and open threads"
```

## Next useful improvements

- Add tags or projects to each note entry.
- Add weekly and monthly rollup commands.
- Add shell completion and shorter aliases.
- Add tests around note insertion and prompt generation.