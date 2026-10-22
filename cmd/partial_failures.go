package cmd

import "github.com/rubrical-works/gh-pmu/internal/api"

// partialFailures collects the steps a command could not complete while it
// carried on with the rest of its work. A command that warns and returns nil
// exits 0 after failing part of its job, so a script cannot tell it from a
// clean run (#938).
type partialFailures struct {
	steps []string
	first error
}

// add records a failed step and its cause.
func (p *partialFailures) add(step string, err error) {
	p.steps = append(p.steps, step)
	if p.first == nil {
		p.first = err
	}
}

// err returns nil when nothing failed, otherwise a PartialSuccessError naming
// done — what the command did complete — and every failed step.
func (p *partialFailures) err(done string) error {
	if len(p.steps) == 0 {
		return nil
	}
	return &api.PartialSuccessError{Done: done, Steps: p.steps, Err: p.first}
}
