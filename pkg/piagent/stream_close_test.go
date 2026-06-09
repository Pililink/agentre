package piagent

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
)

func TestStreamClose(t *testing.T) {
	convey.Convey("Given a pi-agent text probe that already reached agent_end", t, func() {
		runner := &fakeRunner{process: newFakeProcess(t)}
		runner.process.stdout = strings.NewReader(strings.Join([]string{
			`{"type":"response","command":"prompt","success":true}`,
			`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"pong"}}`,
			`{"type":"agent_end","messages":[]}`,
			"",
		}, "\n"))
		runner.process.finishOnSignal(interruptExitError(t))
		client := New(
			WithRPCProcessRunnerForTesting(runner),
			WithKillGrace(time.Second),
		)

		convey.Convey("When Text closes the completed RPC stream, then SIGINT cleanup is not surfaced as failure", func() {
			text, err := client.Text(context.Background(), "ping")

			convey.So(err, convey.ShouldBeNil)
			convey.So(text, convey.ShouldEqual, "pong")
			assert.True(t, runner.process.signaled, "completed text probe should interrupt the lingering RPC process during cleanup")
		})
	})

	convey.Convey("Given a completed pi-agent text probe whose cleanup exits non-zero", t, func() {
		runner := &fakeRunner{process: newFakeProcess(t)}
		runner.process.stdout = strings.NewReader(strings.Join([]string{
			`{"type":"response","command":"prompt","success":true}`,
			`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"pong"}}`,
			`{"type":"agent_end","messages":[]}`,
			"",
		}, "\n"))
		runner.process.finishOnStdinClose(errors.New("exit status 0xc0000409"))
		client := New(
			WithRPCProcessRunnerForTesting(runner),
			WithKillGrace(time.Second),
		)

		convey.Convey("When Text already observed Done, then cleanup failure does not replace the successful result", func() {
			text, err := client.Text(context.Background(), "ping")

			convey.So(err, convey.ShouldBeNil)
			convey.So(text, convey.ShouldEqual, "pong")
		})
	})

	convey.Convey("Given a running pi-agent RPC stream", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), time.Second)

		convey.Convey("When Close interrupts the RPC process and it exits from SIGINT, then Close succeeds", func() {
			proc.finishOnSignal(interruptExitError(t))

			err := stream.Close(context.Background())

			convey.So(err, convey.ShouldBeNil)
			assert.True(t, proc.signaled, "running process should be interrupted during Close")
		})
	})

	convey.Convey("Given a running pi-agent RPC stream", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), time.Second)

		convey.Convey("When Close interrupts the RPC process and it exits with another error, then Close returns that error", func() {
			proc.finishOnSignal(errors.New("exit status 2"))

			err := stream.Close(context.Background())

			convey.So(err, convey.ShouldNotBeNil)
			assert.Contains(t, err.Error(), "exit status 2")
			assert.True(t, proc.signaled, "running process should be interrupted during Close")
		})
	})

	convey.Convey("Given a running pi-agent RPC stream behind a Windows npm cmd shim", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), time.Second)

		convey.Convey("When Close interrupts it and the shim reports exit status 1 without stderr, then Close treats cleanup as successful", func() {
			proc.finishOnSignal(errors.New("exit status 1"))

			err := stream.Close(context.Background())

			if isWindowsCmdInterruptExit(errors.New("exit status 1"), "") {
				convey.So(err, convey.ShouldBeNil)
			} else {
				convey.So(err, convey.ShouldNotBeNil)
			}
			assert.True(t, proc.signaled, "running process should be interrupted during Close")
		})
	})

	convey.Convey("Given a running pi-agent RPC process that exits on stdin EOF", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), 20*time.Millisecond)

		convey.Convey("When Close terminates it, then stdin is closed before force-killing the process", func() {
			proc.finishOnStdinClose(nil)
			proc.finishOnKill(errors.New("exit status 1"))

			err := stream.Close(context.Background())

			convey.So(err, convey.ShouldBeNil)
			assert.True(t, proc.stdin.closed, "Close should close RPC stdin so Node-based shims can exit")
			assert.False(t, proc.killed, "process should not need a forced kill after stdin EOF")
		})
	})
}

type fakeProcess struct {
	t       *testing.T
	stdin   *fakeStdin
	stdout  *strings.Reader
	stderr  *strings.Reader
	done    chan error
	signalC chan os.Signal
	killC   chan struct{}

	signaled bool
	killed   bool
}

func newFakeProcess(t *testing.T) *fakeProcess {
	t.Helper()
	return &fakeProcess{
		t:       t,
		stdin:   newFakeStdin(),
		stdout:  strings.NewReader(""),
		stderr:  strings.NewReader(""),
		done:    make(chan error, 1),
		signalC: make(chan os.Signal, 1),
		killC:   make(chan struct{}, 1),
	}
}

func (f *fakeProcess) rpcProcess() *rpcProcess {
	return &rpcProcess{
		handle: f,
		stdin:  f.stdin,
		lines:  nil,
		stderr: &lockedBuffer{},
		done:   f.done,
	}
}

func (f *fakeProcess) finishOnSignal(err error) {
	f.t.Helper()
	go func() {
		<-f.signalC
		f.done <- err
	}()
}

func (f *fakeProcess) finishOnStdinClose(err error) {
	f.t.Helper()
	go func() {
		<-f.stdin.closeC
		f.done <- err
	}()
}

func (f *fakeProcess) finishOnKill(err error) {
	f.t.Helper()
	go func() {
		<-f.killC
		f.done <- err
	}()
}

func (f *fakeProcess) Stdin() io.Writer  { return f.stdin }
func (f *fakeProcess) Stdout() io.Reader { return f.stdout }
func (f *fakeProcess) Stderr() io.Reader { return f.stderr }

func (f *fakeProcess) Wait() error {
	err, ok := <-f.done
	if !ok {
		return nil
	}
	return err
}

func (f *fakeProcess) Kill() error {
	f.killed = true
	select {
	case f.killC <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeProcess) Signal(sig os.Signal) error {
	f.signaled = true
	select {
	case f.signalC <- sig:
	default:
	}
	return nil
}

func interruptExitError(t *testing.T) error {
	t.Helper()
	return errors.New("signal: interrupt")
}

type fakeRunner struct {
	process *fakeProcess
}

func (r *fakeRunner) Start(context.Context, procOptions) (processHandle, error) {
	return r.process, nil
}

func TestFakeProcessImplementsProcessHandle(t *testing.T) {
	var _ processHandle = (*fakeProcess)(nil)
}

type fakeStdin struct {
	closeC chan struct{}
	closed bool
}

func newFakeStdin() *fakeStdin {
	return &fakeStdin{closeC: make(chan struct{})}
}

func (s *fakeStdin) Write(p []byte) (int, error) { return len(p), nil }

func (s *fakeStdin) Close() error {
	if !s.closed {
		s.closed = true
		close(s.closeC)
	}
	return nil
}
