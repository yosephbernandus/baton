// Package dispatch routes a request to the transport its runtime speaks.
//
// It lives outside internal/transport because the concrete transports import
// that package for the vocabulary; a selector living there would close the
// import cycle. Only this package knows every transport exists.
package dispatch

import (
	"context"

	"github.com/yosephbernandus/baton/internal/acp"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/task"
	"github.com/yosephbernandus/baton/internal/transport"
)

// Mux picks a transport per runtime. Protocol is a property of the runtime, not
// of the pipeline, so a single run can drive one role over ACP and another as a
// subprocess.
type Mux struct {
	cfg  *config.Config
	exec *runner.Runner
	acp  *acp.Transport
}

// New builds a Mux over the configured runtimes.
func New(cfg *config.Config, emitter *events.Emitter, store *task.Store) *Mux {
	return &Mux{
		cfg:  cfg,
		exec: runner.New(cfg, emitter, store),
		acp:  acp.New(cfg, nil),
	}
}

// Exec exposes the subprocess transport for callers that need it directly,
// such as process teardown.
func (m *Mux) Exec() *runner.Runner { return m.exec }

func (m *Mux) transportFor(runtimeName string) transport.Transport {
	if rt, ok := m.cfg.Runtimes[runtimeName]; ok && rt.Protocol == config.ProtocolACP {
		return m.acp
	}
	return m.exec
}

// Capabilities reports what the runtime's transport can do.
func (m *Mux) Capabilities(runtimeName string) transport.Caps {
	return m.transportFor(runtimeName).Capabilities(runtimeName)
}

// Execute runs one request on the runtime's transport.
func (m *Mux) Execute(ctx context.Context, req transport.Request) (*transport.Result, error) {
	return m.transportFor(req.RuntimeName).Execute(ctx, req)
}
