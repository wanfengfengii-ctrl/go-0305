package store

import "time"

// now returns the current wall-clock time in milliseconds. It is used only to
// seed audit ordering; business logical time is always supplied by callers.
func now() int64 { return time.Now().UnixMilli() }
