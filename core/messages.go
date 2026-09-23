package core

// ---------- messages ----------

// ctrlMsg marks the control messages the router applies to the stack synchronously,
// whether returned from Update or arriving via the queue.
type ctrlMsg interface{ isCtrl() }

func (propagateMsg) isCtrl()   {}
func (statusSetMsg) isCtrl()   {}
func (statusClearMsg) isCtrl() {}

// statusSetMsg sets the status line; the router schedules its clear timer.
type statusSetMsg struct {
	str       string
	wrLog     bool
	forceShow bool
}

func SetStatus(line string) Action {
	return Action{Msg: statusSetMsg{str: line}}
}

func SetStatusAndLog(line string, forceShow ...bool) Action {
	shw := GetOptional(false, forceShow...)
	return Action{Msg: statusSetMsg{str: line, wrLog: true, forceShow: shw}}
}

// StatusErr is shorthand for the ubiquitous SetStatusAndLog("error: " + err.Error()).
func StatusErr(err error) Action { return SetStatusAndLog("error: " + err.Error()) }

// SeqErr reports err on the status line, then runs then.
func SeqErr(err error, then ...Action) Action {
	return Seq(append([]Action{StatusErr(err)}, then...)...)
}

// statusClearMsg is the status auto-clear timer; it clears only if gen is still current.
type statusClearMsg struct{ gen int }

// TaskEvent is one progress line of a streaming task, or its final Done event with an
// error and an opaque Payload for onDone.
type TaskEvent struct {
	Line    string
	Done    bool
	Err     error
	Payload any // consumer-defined result for the terminating (Done) event
}

// propagateMsg carries an opaque payload broadcast to every Receiver.
type propagateMsg struct{ payload any }

// MsgThemeChanged is broadcast by ApplyTheme. An App typically answers it with
// RefreshRoots().
type MsgThemeChanged struct{}
