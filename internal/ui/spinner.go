package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// RunWithSpinner runs fn while rendering a compact spinner on TTY stdout.
func RunWithSpinner(label string, fn func() error) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		if label != "" {
			fmt.Println(label)
		}
		return fn()
	}

	return runSpinner(label, func() (func(string), <-chan error) {
		done := make(chan error, 1)
		go func() {
			done <- fn()
		}()
		return nil, done
	})
}

// RunWithStatusSpinner runs fn while rendering a compact spinner with mutable status text.
func RunWithStatusSpinner(initial string, fn func(update func(string)) error) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(func(string) {})
	}

	updates := make(chan string, 8)
	return runSpinner(initial, func() (func(string), <-chan error) {
		done := make(chan error, 1)
		update := func(status string) {
			select {
			case updates <- status:
			default:
			}
		}
		go func() {
			done <- fn(update)
		}()
		return update, done
	}, updates)
}

func runSpinner(initial string, start func() (func(string), <-chan error), updateCh ...<-chan string) error {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	_, done := start()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	label := initial
	render := func() {
		if strings.TrimSpace(label) == "" {
			fmt.Printf("\r%s", Cyan(frames[i%len(frames)]))
		} else {
			fmt.Printf("\r%s %s", Cyan(frames[i%len(frames)]), label)
		}
		i++
	}
	clear := func() {
		fmt.Print("\r" + strings.Repeat(" ", termWidth()) + "\r")
	}

	render()
	for {
		var updates <-chan string
		if len(updateCh) > 0 {
			updates = updateCh[0]
		}
		select {
		case err := <-done:
			clear()
			return err
		case status, ok := <-updates:
			if ok {
				label = status
				render()
			}
		case <-ticker.C:
			render()
		}
	}
}
