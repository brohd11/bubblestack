package core

// ListDensityProvider opts an app context into a session-wide density preference
// for standard root lists and pickers. Return a stable pointer owned by the app;
// false means expanded, true means compact. Nil leaves density screen-local.
// File panels and custom lists do not participate automatically.
// bubblestack.Run restores this preference from the shared config at startup and
// saves user toggles; directly constructed component hosts remain session-only.
type ListDensityProvider interface {
	ListDensity() *bool
}

// MsgListDensityChanged tells participating screens to reconcile their delegates
// with the shared preference, without rebuilding roots or reloading domain data.
// After changing the preference programmatically, broadcast it with PropagateAll.
type MsgListDensityChanged struct{}
