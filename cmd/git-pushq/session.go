package main

import "github.com/ezcdlabs/pushq/pkg/pushq"

// PushSession is the TUI's contract with the pushq library.
// The TUI calls Start once and reads events from the returned channel until
// a Done event is received. Cancel triggers a graceful self-ejection.
type PushSession interface {
	Start() <-chan pushq.Event
	Cancel()
}
