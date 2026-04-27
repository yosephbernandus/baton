package events

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type Tailer struct {
	path string
}

func NewTailer(path string) *Tailer {
	return &Tailer{path: path}
}

func (t *Tailer) Tail(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 64)

	f, err := os.Open(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			f, err = os.Create(t.path)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	if err := watcher.Add(t.path); err != nil {
		_ = f.Close()
		_ = watcher.Close()
		return nil, err
	}

	go func() {
		defer close(ch)
		defer f.Close()         //nolint:errcheck
		defer watcher.Close()   //nolint:errcheck

		reader := bufio.NewReader(f)

		readLines := func() {
			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					if err != io.EOF {
						return
					}
					break
				}
				var ev Event
				if json.Unmarshal(line, &ev) == nil {
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		readLines()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) {
					readLines()
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return ch, nil
}

func (t *Tailer) ReadAll() ([]Event, error) {
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var events []Event
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		var ev Event
		if json.Unmarshal(scanner.Bytes(), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events, nil
}
