# Pull Request

## Closes
- #171

## Summary

This change preserves and publishes the number of distinct candidate paths considered for each priced rung.

## What changed

- count distinct Horizon paths before dropping the lower-ranked candidates
- carry the count through route pricing into the published quote model
- expose the value on rung and recommended quote JSON as `path_count`
- add a regression test covering the wire output contract

## Why

The previous behavior discarded all but the best path for a rung, which meant the published corridor data lost legitimate path diversity even when Horizon returned several viable routes.

## Verification

- ok  	github.com/Wayfare-labs/wayfare	(cached)
- ok  	github.com/Wayfare-labs/wayfare/analysis	(cached)
- ok  	github.com/Wayfare-labs/wayfare/anchor	(cached)
- ok  	github.com/Wayfare-labs/wayfare/asset	(cached)
- ok  	github.com/Wayfare-labs/wayfare/checks	(cached)
- ok  	github.com/Wayfare-labs/wayfare/cmd/hop-analysis	(cached)
- ok  	github.com/Wayfare-labs/wayfare/cmd/ladder	(cached)
- ok  	github.com/Wayfare-labs/wayfare/cmd/wayfared	(cached)
- ok  	github.com/Wayfare-labs/wayfare/dex	(cached)
- ok  	github.com/Wayfare-labs/wayfare/monitor	(cached)
- ok  	github.com/Wayfare-labs/wayfare/refrate	(cached)
- ok  	github.com/Wayfare-labs/wayfare/route	(cached)
- ok  	github.com/Wayfare-labs/wayfare/runstore	(cached)
- ok  	github.com/Wayfare-labs/wayfare/sep38	(cached)
- ok  	github.com/Wayfare-labs/wayfare/server	(cached)
- ok  	github.com/Wayfare-labs/wayfare/snapshot	(cached)
- ok  	github.com/Wayfare-labs/wayfare/transport	(cached)
