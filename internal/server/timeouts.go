package server

import "time"

// defaultCronTimeout caps a single /cron/{name} invocation. Lambda free
// tier runs at most 1 instance, so a long cron serializes all other crons
// behind it and amplifies any DoS via the cron route. 60s is the budget; long
// crons must publish to PubSub and exit fast.
const defaultCronTimeout = 60 * time.Second
