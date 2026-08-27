package config

import "testing"

// "auto" is baton's own sentinel — `baton setup` offers it as "let runtime
// choose" — but it reaches transports as an ordinary model value. Passing it on
// as a model name either errors or silently selects nothing: over ACP every
// phase logged `model "auto" not offered by the agent`, and over exec it became
// a literal `--model auto`.
func TestAutoMeansNoSelection(t *testing.T) {
	rt := RuntimeConfig{Models: []string{"sonnet", "haiku"}}
	if rt.ModelSelected(ModelAuto) {
		t.Error(`ModelSelected("auto")=true, want false: it means leave the runtime on its default`)
	}
}

func TestEmptyMeansNoSelection(t *testing.T) {
	rt := RuntimeConfig{Models: []string{"sonnet"}}
	if rt.ModelSelected("") {
		t.Error(`ModelSelected("")=true, want false`)
	}
}

func TestNamedModelIsSelected(t *testing.T) {
	rt := RuntimeConfig{Models: []string{"sonnet", "haiku"}}
	if !rt.ModelSelected("sonnet") {
		t.Error(`ModelSelected("sonnet")=false, want true`)
	}
	// A model the runtime did not declare is still a selection: the config's
	// model list is documentation, not an allowlist, and a transport reports
	// its own rejection.
	if !rt.ModelSelected("some-model-not-listed") {
		t.Error("an undeclared model must still count as a selection")
	}
}

// A runtime that genuinely offers a model called "auto" declares it, and then
// the name is real for that runtime and passes through.
func TestAutoIsRealWhenTheRuntimeDeclaresIt(t *testing.T) {
	rt := RuntimeConfig{Models: []string{"auto", "sonnet"}}
	if !rt.ModelSelected(ModelAuto) {
		t.Error(`ModelSelected("auto")=false, want true when the runtime lists it as a model`)
	}
}

func TestRuntimeWithNoModelListTreatsAutoAsSentinel(t *testing.T) {
	rt := RuntimeConfig{}
	if rt.ModelSelected(ModelAuto) {
		t.Error(`ModelSelected("auto")=true, want false when nothing declares it`)
	}
}
