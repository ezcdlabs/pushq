package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ezcdlabs/pushq/internal/display"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

func main() {
	name := "happy-path"
	if len(os.Args) >= 3 && os.Args[1] == "--play" {
		name = os.Args[2]
	} else if len(os.Args) >= 2 && os.Args[1] == "--play" {
		// use default name
	} else if len(os.Args) >= 2 {
		fmt.Fprintf(os.Stderr, "usage: demo --play [scenario]\n")
		fmt.Fprintf(os.Stderr, "available scenarios:\n")
		for k := range allScenarios {
			fmt.Fprintf(os.Stderr, "  %s\n", k)
		}
		os.Exit(1)
	}

	s, ok := allScenarios[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown scenario: %q\n", name)
		os.Exit(1)
	}

	if err := playScenario(s); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// scenarioSession implements display.PushSession by emitting scripted events
// with configurable hold durations between them.
type scenarioSession struct {
	frames []Frame
}

func (s *scenarioSession) Start() <-chan pushq.Event {
	ch := make(chan pushq.Event)
	go func() {
		for _, f := range s.frames {
			ch <- f.Event
			if f.Hold > 0 {
				time.Sleep(f.Hold)
			}
		}
		close(ch)
	}()
	return ch
}

func (s *scenarioSession) Cancel() {}

func playScenario(s *Scenario) error {
	// Phase 1: print the prelude to the terminal.
	for _, line := range s.Prelude {
		time.Sleep(line.Delay)
		if line.Typing {
			for i, ch := range []rune(line.Text) {
				fmt.Print(string(ch))
				if i < len([]rune(line.Text))-1 {
					time.Sleep(45 * time.Millisecond)
				}
			}
			fmt.Println()
		} else if line.NoNewline {
			fmt.Print(line.Text)
		} else {
			fmt.Println(line.Text)
		}
	}

	time.Sleep(400 * time.Millisecond)

	// Phase 2: run events through the real display code.
	session := &scenarioSession{frames: s.Frames}
	if err := display.RunInline(session, os.Stdout, "you", false); err != nil {
		return err
	}

	// Phase 3: post-session output.
	time.Sleep(200 * time.Millisecond)
	fmt.Println("\nlanded.")
	return nil
}
