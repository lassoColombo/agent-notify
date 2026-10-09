package subscribe

import (
	"context"
	"fmt"
	"time"

	"github.com/lassoColombo/agent-notify/internal/onewatcher"
	"github.com/lassoColombo/agent-notify/internal/storewatch"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// Run is the loop for a display that owns its process: it watches the store
// and hands onChange a view whenever something the integration asked about
// moves, until the context ends. The first view carries no changes; later ones
// say what moved since the last (R22).
//
// It starts a session-watcher if none holds the lock, so a display started
// before anything else is correct the moment the rest appears.
func Run(ctx context.Context, i Integration, onChange func(session.View) error) error {
	if i.Name == "" {
		return fmt.Errorf("an integration must say what it is called")
	}
	if onChange == nil {
		return fmt.Errorf("%s has nothing to do", i.Name)
	}
	if err := session.ReasonTheseFieldsCannotBeWokenOn(i.WakeOn); err != nil {
		return fmt.Errorf("%s: %w", i.Name, err)
	}

	opened, err := i.core()
	if err != nil {
		return err
	}
	if err := onewatcher.StartIfNobodyIs(opened.Layout, opened.Settings.AgentNotifyBinary); err != nil {
		opened.Logger.Warn("cannot start a session-watcher", "problem", err.Error())
	}
	watch, err := storewatch.Directories(opened.Layout.Sessions(), opened.Layout.Ended())
	if err != nil {
		return err
	}
	defer watch.Close()

	var shown session.LastShown
	for ctx.Err() == nil {
		if view, worth := shown.Replace(opened.WhatIsRunning(i.WantEnded), i.WakeOn); worth {
			if err := onChange(view); err != nil {
				opened.Logger.Warn("rendering", "problem", err.Error())
			}
		}
		for ctx.Err() == nil && !watch.Changed(time.Second) {
		}
	}
	return nil
}

// focusTimeout is generous because core gives each container five seconds of
// its own, and a focus through a window manager and a multiplexer takes
// several.
const focusTimeout = 30 * time.Second

// Focus brings a session to the front through `agent-notify focus-session`,
// which walks the containers in the user's order (§A11.2).
func (i Integration) Focus(key string) error {
	core, err := i.CoreBinary()
	if err != nil {
		return err
	}
	_, err = tool.Run(core, focusTimeout, "focus-session", "--quiet", key)
	return err
}
