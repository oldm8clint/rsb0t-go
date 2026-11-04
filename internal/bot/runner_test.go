package bot

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBot counts calls and lets a test control exactly when Loop starts erroring or how
// many times it should run, without any live session/pathfind/locindex dependency —
// Runner's contract (OnStart once, Loop until stop/error, OnStop exactly once) doesn't need
// a real script to exercise.
type fakeBot struct {
	mu        sync.Mutex
	started   bool
	stopped   bool
	loopCalls int
	startErr  error
	loopErr   error
	// loopErrAfter, if > 0, makes Loop return loopErr only once loopCalls reaches it.
	loopErrAfter int
	// onLoop, if set, runs inside Loop before the call count/err logic, e.g. to call
	// ctx methods under test.
	onLoop func(ctx *Context)
}

func (f *fakeBot) OnStart(ctx *Context) error {
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	return f.startErr
}

func (f *fakeBot) Loop(ctx *Context) error {
	f.mu.Lock()
	f.loopCalls++
	calls := f.loopCalls
	f.mu.Unlock()
	if f.onLoop != nil {
		f.onLoop(ctx)
	}
	if f.loopErrAfter > 0 && calls >= f.loopErrAfter {
		return f.loopErr
	}
	return nil
}

func (f *fakeBot) OnStop(ctx *Context) {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
}

func (f *fakeBot) snapshot() (started, stopped bool, loopCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.stopped, f.loopCalls
}

func newTestContext() *Context {
	return NewContext(nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

// panicBot panics from Loop and/or OnStop on cue, to prove Runner never lets a bot's panic
// escape and take the whole fleet process down with it.
type panicBot struct {
	panicInLoop   bool
	panicInOnStop bool
	stopped       bool
}

func (p *panicBot) OnStart(ctx *Context) error { return nil }

func (p *panicBot) Loop(ctx *Context) error {
	if p.panicInLoop {
		panic("boom from Loop")
	}
	ctx.requestStop()
	return nil
}

func (p *panicBot) OnStop(ctx *Context) {
	p.stopped = true
	if p.panicInOnStop {
		panic("boom from OnStop")
	}
}

func TestRunnerCallsOnStartLoopOnStopInOrder(t *testing.T) {
	fb := &fakeBot{}
	ctx := newTestContext()
	r := NewRunner("Fake", fb, ctx)

	r.Start()
	// Stop immediately; the exact number of Loop calls before the stop is observed is a
	// race by design (that's what makes Stop responsive), so this only asserts the
	// invariants Runner promises: OnStart before OnStop, and OnStop happens exactly once.
	r.Stop()
	r.Wait()

	started, stopped, _ := fb.snapshot()
	if !started {
		t.Fatal("OnStart was never called")
	}
	if !stopped {
		t.Fatal("OnStop was never called")
	}
	if got := r.Status().State; got != StateStopped {
		t.Fatalf("Status().State = %q, want %q", got, StateStopped)
	}
}

func TestRunnerStopsCallingLoopAfterStopRequested(t *testing.T) {
	fb := &fakeBot{}
	ctx := newTestContext()
	fb.onLoop = func(ctx *Context) {
		// Request a stop from inside the very first Loop call and confirm no further
		// Loop call happens afterward.
		ctx.requestStop()
	}
	r := NewRunner("Fake", fb, ctx)
	r.Start()
	r.Wait()

	_, _, loopCalls := fb.snapshot()
	if loopCalls != 1 {
		t.Fatalf("Loop was called %d times after requesting stop on the first call, want exactly 1", loopCalls)
	}
}

func TestRunnerReportsErrorFromLoop(t *testing.T) {
	fb := &fakeBot{loopErrAfter: 1, loopErr: errors.New("boom")}
	ctx := newTestContext()
	r := NewRunner("Fake", fb, ctx)
	r.Start()
	r.Wait()

	st := r.Status()
	if st.State != StateError {
		t.Fatalf("Status().State = %q, want %q", st.State, StateError)
	}
	if st.Err != "boom" {
		t.Fatalf("Status().Err = %q, want %q", st.Err, "boom")
	}
	_, stopped, _ := fb.snapshot()
	if !stopped {
		t.Fatal("OnStop must still run after an error from Loop")
	}
}

func TestRunnerRecoversPanicFromLoop(t *testing.T) {
	pb := &panicBot{panicInLoop: true}
	ctx := newTestContext()
	r := NewRunner("Fake", pb, ctx)
	r.Start()
	r.Wait() // if the panic escaped run's goroutine, the test binary itself would crash here.

	st := r.Status()
	if st.State != StateError {
		t.Fatalf("Status().State = %q, want %q", st.State, StateError)
	}
	if !containsAll(st.Err, "panic", "boom from Loop") {
		t.Fatalf("Status().Err = %q, want it to mention the panic value", st.Err)
	}
	if !pb.stopped {
		t.Fatal("OnStop must still run after Loop panics")
	}
}

func TestRunnerRecoversPanicFromOnStop(t *testing.T) {
	pb := &panicBot{panicInOnStop: true}
	ctx := newTestContext()
	r := NewRunner("Fake", pb, ctx)
	r.Start()
	r.Wait() // if the panic escaped run's goroutine, the test binary itself would crash here.

	st := r.Status()
	if st.State != StateError {
		t.Fatalf("Status().State = %q, want %q", st.State, StateError)
	}
	if !containsAll(st.Err, "panic", "boom from OnStop") {
		t.Fatalf("Status().Err = %q, want it to mention the panic value", st.Err)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestRunnerReportsErrorFromOnStartWithoutCallingLoop(t *testing.T) {
	fb := &fakeBot{startErr: errors.New("setup failed")}
	ctx := newTestContext()
	r := NewRunner("Fake", fb, ctx)
	r.Start()
	r.Wait()

	st := r.Status()
	if st.State != StateError || st.Err != "setup failed" {
		t.Fatalf("Status() = %+v, want error state with the OnStart error", st)
	}
	if _, _, loopCalls := fb.snapshot(); loopCalls != 0 {
		t.Fatalf("Loop was called %d times after OnStart failed, want 0", loopCalls)
	}
}

func TestContextDelayReturnsFalseWhenStopRequestedMidSleep(t *testing.T) {
	ctx := newTestContext()
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctx.requestStop()
	}()
	start := time.Now()
	ok := ctx.Delay(2 * time.Second)
	elapsed := time.Since(start)
	if ok {
		t.Fatal("Delay returned true (not canceled) despite a stop request mid-sleep")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Delay took %v to return after a stop request, want well under its 2s duration", elapsed)
	}
}

func TestContextDelayUntilStopsPollingOnStop(t *testing.T) {
	ctx := newTestContext()
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctx.requestStop()
	}()
	ok := ctx.DelayUntil(func() bool { return false }, 5*time.Second)
	if ok {
		t.Fatal("DelayUntil returned true despite cond never becoming true and a stop request arriving")
	}
}

func TestContextDelayUntilReturnsTrueAssoonAsCondIsTrue(t *testing.T) {
	ctx := newTestContext()
	n := 0
	ok := ctx.DelayUntil(func() bool {
		n++
		return n >= 3
	}, 5*time.Second)
	if !ok {
		t.Fatal("DelayUntil returned false even though cond became true")
	}
}

func TestContextRequestHandoffIsOneShot(t *testing.T) {
	ctx := newTestContext()
	if ctx.TakeHandoffRequest() {
		t.Fatal("a fresh Context already reports a pending hand-off request")
	}
	ctx.RequestHandoff()
	if !ctx.TakeHandoffRequest() {
		t.Fatal("TakeHandoffRequest did not report the request RequestHandoff made")
	}
	if ctx.TakeHandoffRequest() {
		t.Fatal("TakeHandoffRequest reported the same request twice — it should clear on read")
	}
}

func TestContextMuleAtOverride(t *testing.T) {
	ctx := newTestContext()
	if _, ok := ctx.MuleAtOverride(); ok {
		t.Fatal("a fresh Context already reports a muleAt override")
	}
	ctx.SetMuleAt(250)
	if v, ok := ctx.MuleAtOverride(); !ok || v != 250 {
		t.Fatalf("MuleAtOverride = %d, %v; want 250, true", v, ok)
	}
	ctx.SetMuleAt(0)
	if _, ok := ctx.MuleAtOverride(); ok {
		t.Fatal("SetMuleAt(0) did not clear the override")
	}
}

func TestRunnerHandoffAndMuleAtPassThrough(t *testing.T) {
	ctx := newTestContext()
	r := NewRunner("test", &fakeBot{}, ctx)
	r.RequestHandoff()
	if !ctx.TakeHandoffRequest() {
		t.Fatal("Runner.RequestHandoff did not reach its Context")
	}
	r.SetMuleAt(999)
	if v, ok := r.MuleAtOverride(); !ok || v != 999 {
		t.Fatalf("Runner.MuleAtOverride = %d, %v; want 999, true", v, ok)
	}
}

func TestRegisterAndNewRoundTrip(t *testing.T) {
	name := fmt.Sprintf("test-script-%d", time.Now().UnixNano())
	Register(name, func() Bot { return &fakeBot{} })

	b, ok := New(name)
	if !ok {
		t.Fatalf("New(%q) not found after Register", name)
	}
	if _, ok := b.(*fakeBot); !ok {
		t.Fatalf("New(%q) returned %T, want *fakeBot", name, b)
	}

	found := false
	for _, n := range Names() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Names() does not include %q", name)
	}

	if _, ok := New("does-not-exist-" + name); ok {
		t.Fatal("New found a script that was never registered")
	}
}

func TestRegisterPanicsOnDuplicateName(t *testing.T) {
	name := fmt.Sprintf("dup-script-%d", time.Now().UnixNano())
	Register(name, func() Bot { return &fakeBot{} })

	defer func() {
		if recover() == nil {
			t.Fatal("Register did not panic on a duplicate name")
		}
	}()
	Register(name, func() Bot { return &fakeBot{} })
}

func TestIntAndBoolSetting(t *testing.T) {
	m := map[string]interface{}{
		"asInt":     5,
		"asFloat64": float64(7), // what encoding/json produces for a bare number
		"asBool":    true,
		"wrongType": "nope",
	}
	if got := IntSetting(m, "asInt", -1); got != 5 {
		t.Errorf("IntSetting(asInt) = %d, want 5", got)
	}
	if got := IntSetting(m, "asFloat64", -1); got != 7 {
		t.Errorf("IntSetting(asFloat64) = %d, want 7 (JSON numbers decode as float64)", got)
	}
	if got := IntSetting(m, "missing", 42); got != 42 {
		t.Errorf("IntSetting(missing) = %d, want default 42", got)
	}
	if got := IntSetting(m, "wrongType", 9); got != 9 {
		t.Errorf("IntSetting(wrongType) = %d, want default 9", got)
	}
	if got := BoolSetting(m, "asBool", false); got != true {
		t.Errorf("BoolSetting(asBool) = %v, want true", got)
	}
	if got := BoolSetting(m, "missing", true); got != true {
		t.Errorf("BoolSetting(missing) = %v, want default true", got)
	}
}
