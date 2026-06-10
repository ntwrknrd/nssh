package broker

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ntwrknrd/nssh/internal/repl"
)

const defaultSuggestionLimit = 8

type request struct {
	Type string `json:"type"`
	Line string `json:"line,omitempty"`
}

type broker struct {
	opts repl.Options
	enc  *json.Encoder
	mu   sync.Mutex

	activeMu sync.Mutex
	active   *activeBatch
}

type activeBatch struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type batchStatus struct {
	total   int
	running int
	done    int
	failed  int
	pending int
}

func Run(ctx context.Context, opts repl.Options) error {
	opts = normalizeOptions(opts)
	b := &broker{
		opts: opts,
		enc:  json.NewEncoder(opts.Out),
	}
	if err := b.emit(map[string]any{"type": "ready"}); err != nil {
		return err
	}

	scanner := bufio.NewScanner(b.opts.In)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if emitErr := b.emitError(fmt.Errorf("decode request: %w", err)); emitErr != nil {
				return emitErr
			}
			continue
		}
		switch req.Type {
		case "suggest":
			if err := b.handleSuggest(req.Line); err != nil {
				return err
			}
		case "history_load":
			if err := b.handleHistoryLoad(); err != nil {
				return err
			}
		case "history_append":
			if err := b.handleHistoryAppend(req.Line); err != nil {
				return err
			}
		case "submit":
			if err := b.handleSubmit(ctx, req.Line); err != nil {
				return err
			}
		case "cancel":
			b.cancelActive()
		case "exit":
			b.waitActive()
			return nil
		default:
			if err := b.emitError(fmt.Errorf("unknown request type %q", req.Type)); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	b.waitActive()
	return nil
}

func normalizeOptions(opts repl.Options) repl.Options {
	if opts.Concurrency <= 0 {
		opts.Concurrency = repl.DefaultConcurrency
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Resolver == nil {
		opts.Resolver = repl.DefaultTargetResolver{}
	}
	if opts.Runner == nil {
		opts.Runner = repl.SSHCommandRunner{}
	}
	if opts.History == nil {
		opts.History = repl.DefaultHistoryStore()
	}
	return opts
}

func (b *broker) handleSuggest(line string) error {
	suggester, ok := b.opts.Resolver.(repl.HostSuggester)
	if !ok {
		return b.emit(map[string]any{"type": "suggestions", "suggestions": []string{}})
	}
	suggestions, err := repl.SuggestTargetInputs(line, suggester, defaultSuggestionLimit)
	if err != nil {
		return b.emitError(err)
	}
	return b.emit(map[string]any{"type": "suggestions", "suggestions": suggestions})
}

func (b *broker) handleHistoryLoad() error {
	lines, err := b.opts.History.Load()
	if err != nil {
		return b.emitError(err)
	}
	return b.emit(map[string]any{"type": "history", "lines": lines})
}

func (b *broker) handleHistoryAppend(line string) error {
	if err := b.opts.History.Append(line); err != nil {
		return b.emitError(err)
	}
	return nil
}

func (b *broker) handleSubmit(parent context.Context, line string) error {
	b.activeMu.Lock()
	if b.active != nil {
		b.activeMu.Unlock()
		return b.emitError(errors.New("batch already active"))
	}

	req, err := repl.ParseLine(line)
	if err != nil {
		b.activeMu.Unlock()
		return b.emitError(err)
	}
	targets, err := repl.ResolveTargets(req, b.opts.Resolver)
	if err != nil {
		b.activeMu.Unlock()
		return b.emitError(err)
	}

	ctx, cancel := context.WithCancel(parent)
	active := &activeBatch{cancel: cancel, done: make(chan struct{})}
	b.active = active
	b.activeMu.Unlock()

	status := batchStatus{total: len(targets) * len(req.Commands), pending: len(targets) * len(req.Commands)}
	if err := b.emitStatus(status); err != nil {
		cancel()
		return err
	}

	go b.runBatch(ctx, active, targets, req.Commands, status)
	return nil
}

func (b *broker) runBatch(ctx context.Context, active *activeBatch, targets []string, commands []string, status batchStatus) {
	defer close(active.done)
	defer b.clearActive(active)

	started := make(map[int]bool, len(targets)*len(commands))
	for event := range repl.ExecuteCommandFanoutStream(ctx, targets, commands, b.opts.Concurrency, b.opts.Runner) {
		eventIndex := batchEventIndex(event, len(targets))
		switch event.Kind {
		case repl.FanoutStarted:
			if !started[eventIndex] {
				started[eventIndex] = true
				status.running++
				if status.pending > 0 {
					status.pending--
				}
			}
			_ = b.emit(map[string]any{
				"type":    "started",
				"batch":   event.Batch,
				"index":   event.Index,
				"host":    event.Host,
				"command": event.Command,
			})
		case repl.FanoutCompleted:
			if started[eventIndex] && status.running > 0 {
				status.running--
			} else if !started[eventIndex] && status.pending > 0 {
				status.pending--
			}
			status.done++
			if event.Result.Err != nil || event.Result.ExitCode != 0 {
				status.failed++
			}
			_ = b.emitCompleted(event)
		}
	}
	_ = b.emitStatus(status)
}

func batchEventIndex(event repl.FanoutEvent, targetCount int) int {
	batch := event.Batch
	if batch <= 0 {
		batch = 1
	}
	return (batch-1)*targetCount + event.Index
}

func (b *broker) emitCompleted(event repl.FanoutEvent) error {
	message := ""
	if event.Result.Err != nil {
		message = event.Result.Err.Error()
	}
	return b.emit(map[string]any{
		"type":      "completed",
		"batch":     event.Batch,
		"index":     event.Index,
		"host":      event.Result.Host,
		"command":   event.Result.Command,
		"stdout":    base64.StdEncoding.EncodeToString(event.Result.Output),
		"exit_code": event.Result.ExitCode,
		"error":     message,
	})
}

func (b *broker) emitStatus(status batchStatus) error {
	return b.emit(map[string]any{
		"type":    "status",
		"running": status.running,
		"done":    status.done,
		"failed":  status.failed,
		"pending": status.pending,
		"total":   status.total,
	})
}

func (b *broker) cancelActive() {
	b.activeMu.Lock()
	active := b.active
	b.activeMu.Unlock()
	if active != nil {
		active.cancel()
	}
}

func (b *broker) waitActive() {
	b.activeMu.Lock()
	active := b.active
	b.activeMu.Unlock()
	if active != nil {
		<-active.done
	}
}

func (b *broker) clearActive(active *activeBatch) {
	b.activeMu.Lock()
	defer b.activeMu.Unlock()
	if b.active == active {
		b.active = nil
	}
}

func (b *broker) emitError(err error) error {
	return b.emit(map[string]any{"type": "error", "message": err.Error()})
}

func (b *broker) emit(event map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.enc.Encode(event)
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}
