package core

// ListDensityProvider opts an app into a session-wide density for root lists and pickers:
// return a stable app-owned pointer (true is compact); nil keeps density per screen.
// bubblestack.Run restores and saves it through the shared config.
type ListDensityProvider interface {
	ListDensity() *bool
}

// MsgListDensityChanged tells participating screens to re-sync their delegates with the
// shared preference. Broadcast it after changing the preference in code.
type MsgListDensityChanged struct{}
