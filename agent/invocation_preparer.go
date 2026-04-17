//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package agent

// InvocationPreparer is an optional hook for agents that need to adjust an
// invocation before it starts running or before transfer-scoped events are
// emitted on their behalf.
//
// This is primarily useful for router/wrapper agents that want to preserve a
// stable agent identity while rewriting runtime details such as the target
// execution agent or event filter key.
type InvocationPreparer interface {
	PrepareInvocation(inv *Invocation)
}

// PrepareInvocationForAgent applies an agent's optional invocation-preparation
// hook when implemented.
func PrepareInvocationForAgent(ag Agent, inv *Invocation) {
	if ag == nil || inv == nil {
		return
	}
	preparer, ok := ag.(InvocationPreparer)
	if !ok {
		return
	}
	preparer.PrepareInvocation(inv)
}
