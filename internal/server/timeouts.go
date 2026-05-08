package server

import "time"

// defaultCronTimeout caps a single /cron/{name} invocation. Cloud Run request
// timeout is 60 minutes max, but we keep crons under our HTTP read timeout so
// runaway handlers cannot pin an instance.
const defaultCronTimeout = 5 * time.Minute
