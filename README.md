# reddit-cli (`reddit`)

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An agent-facing CLI for reading Reddit from the terminal: subreddit listings,
keyword search, full comment trees, user activity and subreddit discovery. The
agent runs `reddit <command>` from Bash and reads token-lean Markdown
(`--json`/`--format` for machines). One static binary, no runtime, and — with
the default source — no credentials.

Sibling of [`exa-cli`](https://github.com/iamnikolie/exa-cli),
[`slack-cli`](https://github.com/iamnikolie/slack-cli),
[`fibery-cli`](https://github.com/iamnikolie/fibery-cli) and
[`gitlab-cli`](https://github.com/iamnikolie/gitlab-cli): same doctrine — plain
text on stdout, nothing costs context until it is called.

> Unofficial, community-built tool. Not affiliated with, endorsed by, or supported by Reddit, Inc. or the Arctic Shift project.

## Data source

Reddit closed self-service Data API keys in late 2025 and blocks anonymous
JSON reads, so the default source is [Arctic Shift](https://arctic-shift.photon-reddit.com)
— a public, no-auth archive of Reddit that ingests posts and comments within
~1–2 minutes of posting. Know its shape before you rely on it:

- `score` and `num_comments` are placeholders (0/1) until ~36 hours after
  posting, then match Reddit. Sorting is by date; `top` and `--sort score`
  re-rank client-side, so very fresh posts rank low.
- Keyword search is scoped: always by subreddit(s) or author, never global,
  and the archive takes one keyword per query — the CLI applies the rest of
  a multi-word query itself over the newest 100 hits.
- Read-only: no inbox, no voting, no posting. No live `hot` ranking.
- It is a free community service with no uptime guarantee. The CLI keeps the
  source behind a `--source` switch so another backend can be added later.

## Install

**From source** (Go 1.23+) — `make install` symlinks the binary, so a later
`make build` updates the installed CLI without reinstalling:

```bash
git clone https://github.com/iamnikolie/reddit-cli.git
cd reddit-cli
make install        # symlink → ~/.local/bin/reddit
```

**With Go** — note `go install` names the binary after the module, so rename it:

```bash
go install github.com/iamnikolie/reddit-cli@latest
mv "$(go env GOPATH)/bin/reddit-cli" "$(go env GOPATH)/bin/reddit"
```

Check what you got with `reddit version`.

## Usage

```bash
reddit sub golang -n 20                       # newest posts
reddit sub golang,rust --after 24h            # several subreddits, merged by date
reddit sub golang top --after 30d -n 15       # best of a window (by archived score)

reddit search "cobra urfave" --sub golang --after 1y
reddit search "sqlite wal" --sub golang --comments --after 90d
reddit search "cobra viper" --sub golang --in title
reddit search "launch" --author spez

reddit post 1wro60n                           # post + 60 best comments, 4 levels deep
reddit post <reddit url> --top 0 --depth 0    # everything
reddit post 1wro60n --ids                     # comment ids for --parent drill-down
reddit post 1wro60n --parent kx0abc1          # one subtree

reddit user spez                              # overview: active subs + latest posts/comments
reddit user spez comments --sub programming -n 50

reddit subs claude --min-subscribers 500      # find communities
reddit api /api/posts/search/aggregate -q aggregate=author -q subreddit=golang -q after=30d
```

Post search: the archive handles one keyword per query, so the CLI sends the
longest word and matches the remaining words and `"quoted phrases"` itself
over the newest 100 hits (all must appear; `--in title` restricts to titles).
Comment search has full syntax: words ANDed, `"quoted phrase"`, `a OR b`,
`-excluded`. Dates accept `2026-09-01`, epoch seconds, or offsets like `12h`, `7d`, `3m`, `1y`.

### Output

Markdown by default; `--json` for the archive records (a thread nests
`replies`); `--format table|csv|tsv` with `--fields id,title,score` for
projections. Advisories (partial results, placeholder scores) go to stderr.

A thread is rendered as an indented list, siblings sorted by score:

```
# What do you use to create CLI tools/apps?
r/golang · u/Least_Chicken_9561 · 2026-09-27 16:11 (18h) · ↑1 · 0 comments · id 1wro60n
https://www.reddit.com/r/golang/comments/1wro60n/

## Comments (36 archived, showing 12; sort score, depth ≤ 4)
- [50] u/SolFlorus · 18h: I like this library: https://github.com/urfave/cli — Cobra always felt heavy to me.
  - [4] u/lllamaboy · 18h: Seconded. But I also like the look of Kong as well.
- [30] u/canarydev · 18h: standard library mostly. for tui bubble tea, bubbles, and lip gloss

_24 comments hidden (--top 0, --depth 0, --parent <id> --ids)._
```

### For agents

`reddit skill` prints the embedded agent reference (commands, workflow,
limits). Drop it into a skill file or point the agent at the binary.

## Configuration

Nothing is required. Optional settings live in
`~/.reddit-cli/<profile>/config.yaml` (`reddit config show`):

```yaml
user_agent: macos:my-tool:v1 (by /u/me)     # default: reddit-cli/<version>
archive_url: https://arctic-shift.photon-reddit.com
```

Environment overrides: `REDDIT_CONFIG` (profile), `REDDIT_ARCHIVE_URL`,
`REDDIT_USER_AGENT`. Profiles: `reddit --config work ...`.

## Being a good citizen

The archive is run by one person on donations. The CLI requests only the
fields it needs, fans out at most three subreddits at a time, waits out short
rate limits and retries a timed-out query once. For bulk work use the
archive's monthly dumps instead of the API.

## Development

```bash
make build      # ./reddit
make test
make vet
make fmt
```

## License

[MIT](LICENSE)
