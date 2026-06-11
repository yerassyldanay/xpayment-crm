package assistant

import "errors"

// ErrNoPublishedConfig is returned when HandleMessage runs before any published
// snapshot exists. The webhook handler acknowledges with 200 and skips drafting.
var ErrNoPublishedConfig = errors.New("assistant: no published config snapshot loaded")
