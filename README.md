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
