// Package replay is the two halves of working on a display without an agent:
// write down what happens, and play it back.
//
// A recording of a real day is the only honest test input a display has —
// invented records are records somebody who already knows the answer wrote.
// Playing one back needs no store, no agents and no session-watcher, which is
// what makes it something an integration author in another repository can
// actually run.
package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// RecordedChange is one line of a recording: how long after the previous one it
// arrived, and what the session looked like.
//
// It is made deliberately, by `record`, rather than accumulated always. An
// always-on log of every transition is the thing D-10 refused; a recording is
// something a person asked for, with a beginning and an end (§A16).
type RecordedChange struct {
	AfterMS int64          `json:"after_ms"`
	Session session.Record `json:"session"`
	// Event is what the agent-integration reported, when the change carried
	// one. Kept because a recording that drops it can never exercise the one
	// display that reads it, and a notifier replayed against an eventless
	// recording is a notifier tested against something it will never meet.
	//
	// Absent from recordings made before this field existed, which is what
	// omitempty is for: they play back exactly as they did.
	Event session.Event `json:"event,omitempty"`
}

// Record writes what happens to a file, so that it can be played back
// into a display later with no agents involved.
func Record() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "record",
		Short: "write what happens, to replay into a display later",
		Long: `Connects as a display would and writes every view it is handed, with the
time it arrived. A recording of a real day is how a display gets worked on
without waiting for agents to do something interesting.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(record(output))
		},
	}
	command.Flags().StringVar(&output, "output", "", "where to write; default is stdout")
	_ = command.MarkFlagFilename("output", "jsonl", "json")
	return command
}

func record(output string) int {
	writing := io.WriteCloser(os.Stdout)
	if output != "" {
		file, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, paths.FileMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify record: %v\n", err)
			return 1
		}
		defer file.Close()
		writing = file
	}
	encoder := json.NewEncoder(writing)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	last := time.Now()
	count := 0
	err := subscribe.Run(ctx, subscribe.Integration{
		Name: "record", Roles: []string{"display"}, WantEnded: true,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		OnChange: func(view subscribe.View) error {
			for _, change := range view.Changed {
				now := time.Now()
				if err := encoder.Encode(RecordedChange{
					AfterMS: now.Sub(last).Milliseconds(),
					Session: change.Record, Event: change.Event,
				}); err != nil {
					return err
				}
				last = now
				count++
			}
			return nil
		},
	})
	fmt.Fprintf(os.Stderr, "recorded %d moment(s)\n", count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify record: %v\n", err)
		return 1
	}
	return 0
}

// Replay plays a recording into whatever connects, with no store, no
// agents and no session-watcher.
func Replay() *cobra.Command {
	var speed float64
	var again bool
	var waiting time.Duration
	command := &cobra.Command{
		Use:   "replay <recording>",
		Short: "play a recording, with no agents and no store",
		Long: `Takes the singleton lock, which means a real session-watcher cannot be
running at the same root. That is correct rather than awkward: a display being
fed a recording must not also be fed reality.`,
		Args: cobra.ExactArgs(1),
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(replay(arguments[0], speed, again, waiting))
		},
	}
	command.Flags().Float64Var(&speed, "speed", 1,
		"how much faster than recorded; 0 plays with no waiting")
	command.Flags().BoolVar(&again, "loop", false, "start over when the recording ends")
	command.Flags().DurationVar(&waiting, "wait-for", 10*time.Second,
		"how long to wait for something to connect")
	return command
}

func replay(recording string, speed float64, again bool, waiting time.Duration) int {
	moments, err := readRecording(recording)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify replay: %v\n", err)
		return 1
	}

	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify replay: %v\n", err)
		return 1
	}
	root := layout.Root
	if root == "" {
		fmt.Fprintln(os.Stderr,
			"agent-notify replay: set AGENT_NOTIFY_ROOT first, so that a recording is never\n"+
				"played into the same place your real sessions live.")
		return 2
	}

	fake, err := subscribe.StartFake(root, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify replay: %v\n", err)
		return 1
	}
	defer fake.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "replaying %d moment(s) at %s; point a display at AGENT_NOTIFY_ROOT=%s\n",
		len(moments), root, root)
	if !waitForADisplayToConnect(ctx, fake, waiting) {
		fmt.Fprintln(os.Stderr, "nothing connected; playing anyway")
	}

	for {
		for _, one := range moments {
			if ctx.Err() != nil {
				return 0
			}
			if speed > 0 && one.AfterMS > 0 {
				pause := time.Duration(float64(one.AfterMS)/(speed)) * time.Millisecond
				select {
				case <-ctx.Done():
					return 0
				case <-time.After(pause):
				}
			}
			fake.Reported(one.Session, one.Event)
		}
		if !again {
			return 0
		}
	}
}

func waitForADisplayToConnect(ctx context.Context, fake *subscribe.Fake, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if len(fake.Connected()) > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func readRecording(path string) ([]RecordedChange, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var moments []RecordedChange
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var one RecordedChange
		if err := json.Unmarshal(scanner.Bytes(), &one); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		moments = append(moments, one)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(moments) == 0 {
		return nil, fmt.Errorf("%s has nothing in it", path)
	}
	return moments, nil
}
