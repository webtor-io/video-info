# video-info
Gathers additional info for torrent's video-content from public sources (OpenSubtitles, etc...)

# Usage

```
% ./video-info --help
NAME:
   video-info - Generates extra video info

USAGE:
   video-info [global options] command [command options] [arguments...]

VERSION:
   0.0.1

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --probe-host value  probe listening host
   --probe-port value  probe listening port (default: 8081)
   --host value        listening host
   --port value        http listening port (default: 8080)
   --redis-host value  redis host (default: "localhost") [$REDIS_MASTER_SERVICE_HOST, $ REDIS_SERVICE_HOST]
   --redis-port value  redis port (default: 6379) [$REDIS_MASTER_SERVICE_PORT, $ REDIS_SERVICE_PORT]
   --help, -h          show help
   --version, -v       print the version
```

# HTTP API

Both endpoints describe one file, named by the headers `X-Source-Url` (the seeder
URL used to compute the OpenSubtitles moviehash), `X-Info-Hash` and `X-Path`
(the cache keys). Search hints come from the query string: `imdb-id`, and
`season` + `episode` together — a half-specified pair is ignored. `purge=true`
bypasses every cache.

## `GET /subtitles.json`

Returns the track listing as a JSON array (never `null`), ranked and capped at
three tracks per language.

| Status | Meaning |
|---|---|
| `200` with a non-empty array | tracks found |
| `200` with `[]` and no `Retry-After` | the file has no subtitles |
| `200` with `[]` and `Retry-After: 5` | **not ready** — a search leg failed (the seeder could not serve the head/tail bytes in time, the client went away, the API errored). Ask again |
| `404` | the request named neither a file nor a title, so there is no resource to report on |

### Track fields

| Field | Meaning |
|---|---|
| `srclang`, `label` | ISO 639-1 code and its English name |
| `src` | path of the track body, `/opensubtitles/<id>.vtt` |
| `format` | always `vtt` |
| `id` | OpenSubtitles subtitle id; the id in `src` |
| `source` | **how confident the sync is for this track**: `hash` when OpenSubtitles matched this exact file, `imdb` when the track only belongs to the same title |
| `moviehash_match` | the raw flag `source` is derived from |
| `release`, `fps`, `hi`, `downloads` | omitted when zero |

`source` describes the *track*, not the search leg that found it. A moviehash
search returns the tracks of the whole movie and flags only the ones that
actually matched the hash, so a hash-leg track without `moviehash_match` is no
better synced to this release than an imdb result and is reported as `imdb`.
Ranking still puts the matched ones first.

A failed leg is deliberately *not* a `404`: browsers, CDNs and `web-ui` read
`404` as "this listing does not exist", while the real state is "come back in a
moment". The cause is logged as the structured field `reason`
(`hash_timeout`, `hash_error`, `imdb_error`, `client_gone`, `no_query`) so the
empty answers can be split by cause. When both legs fail the two causes are
joined with `+` (`hash_timeout+imdb_error`) rather than one overwriting the
other — that combination is the one worth seeing.

## `GET /opensubtitles/<id>.<format>`

Returns the body of one track, converted to WebVTT.

| Status | Meaning |
|---|---|
| `200` | the track body |
| `400` | the path or the id does not parse |
| `404` | the id belongs to no track of this file, or the request named neither a file nor a title |
| `503` with `Retry-After: 5` | **not ready** — a search leg failed, so the track may exist and could not be read yet. Ask again |

The same distinction as on the listing, for the same reason: on a cold torrent
both legs can be unavailable at once (the 24h hash entry expired, the seeder is
cold, and the request carries no `imdb-id`), and the track does exist. The
`Retry-After` header is named in `Access-Control-Expose-Headers`, because the
reader of this response is the player in the browser.
