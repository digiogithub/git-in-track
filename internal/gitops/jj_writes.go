package gitops

import (
	"context"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The write half of the Jujutsu backend.
//
// GIT-US-0040 delivered the read half only. Every write is still refused, with
// the same code and the same wording GIT-US-0038 established, so that a jj
// repository behaves identically whether the jj binary is installed or not: it
// reads, and it never writes behind the user's back.
//
// GIT-US-0041 replaces the bodies below with real jj invocations — `jj commit
// -m … -- <paths>`, `jj git fetch`, `jj rebase -d …`, `jj git push`, `jj undo`
// and `jj resolve`. Nothing outside this file has to change when it does: the
// backend already satisfies the whole of the neutral interface, and
// Capabilities.Writes is the single flag that turns the write half on.

// Commit refuses to record the working-copy commit. It is GIT-US-0041.
func (b *jujutsuBackend) Commit(_ context.Context, _ CommitRequest) (CommitResult, error) {
	return CommitResult{}, refuseJujutsu("commit", b.path, core.JujutsuCommitCommand)
}

// Fetch refuses to bring remote work in. It is GIT-US-0041.
func (b *jujutsuBackend) Fetch(_ context.Context, _ FetchRequest) (FetchResult, error) {
	return FetchResult{}, refuseJujutsu("fetch", b.path, core.JujutsuFetchCommand)
}

// Integrate refuses to rebase. It is GIT-US-0041.
func (b *jujutsuBackend) Integrate(_ context.Context, _ IntegrateRequest) (IntegrateResult, error) {
	return IntegrateResult{}, refuseJujutsu("integrate", b.path, core.JujutsuRebaseCommand)
}

// Push refuses to publish a bookmark. It is GIT-US-0041.
func (b *jujutsuBackend) Push(_ context.Context, _ PushRequest) (PushResult, error) {
	return PushResult{}, refuseJujutsu("push", b.path, core.JujutsuPushCommand)
}

// Undo refuses to revert an operation. It is GIT-US-0041.
func (b *jujutsuBackend) Undo(_ context.Context) error {
	return refuseJujutsu("abort", b.path, core.JujutsuUndoCommand)
}

// Resume refuses; jj records conflicts inside commits and has no half-finished
// operation to carry forward, which is what Integration.Resume already says.
func (b *jujutsuBackend) Resume(_ context.Context) (IntegrateResult, error) {
	return IntegrateResult{}, refuseJujutsu("continue", b.path, core.JujutsuResolveCommand)
}

// ResolvePath refuses to record a resolution. It is GIT-US-0041; reading the
// three sides of the conflict, which is what the resolver opens with, works
// today through ConflictFile.
func (b *jujutsuBackend) ResolvePath(_ context.Context, _ ResolveRequest) (ResolveResult, error) {
	return ResolveResult{}, refuseJujutsu("resolve", b.path, core.JujutsuResolveCommand)
}
