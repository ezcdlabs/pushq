package main

import (
	"testing"
	"time"
)

// TestScenariosDoNotPanic steps through every frame of every scenario and
// calls View(), catching any index-out-of-bounds or nil-pointer panics.
func TestScenariosDoNotPanic(t *testing.T) {
	for name, s := range allScenarios {
		t.Run(name, func(t *testing.T) {
			var scr []screen
			var delays []time.Duration
			for _, f := range s.Frames {
				scr = append(scr, f.Screen)
				delays = append(delays, f.Hold)
			}

			m := model{
				screens:  scr,
				delays:   delays,
				autoplay: true,
				width:    160,
				height:   44,
			}

			for i := range scr {
				m.index = i
				_ = m.View()
			}

			// also call View() with index past the end — the quit path
			m.index = len(scr)
			_ = m.View()
		})
	}
}
